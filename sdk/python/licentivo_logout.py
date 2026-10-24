"""Durable, unlink-only logout recovery shared by the official Python client."""
import json
import hashlib
import os
from pathlib import Path
import tempfile
import threading
import time
import urllib.request

REVOKED = frozenset(['LICENSE_REVOKED','LICENSE_SUSPENDED','LICENSE_EXPIRED','DEVICE_RELEASED','DEVICE_NOT_ACTIVE','DEVICE_CREDENTIAL_REVOKED','DEVICE_CREDENTIAL_INVALID','AUTHORIZATION_SESSION_REVOKED','AUTHORIZATION_SESSION_EXPIRED','LAUNCH_LOGIN_REQUIRED','LAUNCH_ENTITLEMENT_REVOKED','LAUNCH_ENTITLEMENT_EXPIRED'])

def revoked(error):
    return getattr(error, 'code', '') in REVOKED

def _binding(ticket):
    return f"{ticket['activation_id']}:{ticket['expected_generation']}" if ticket else ''

def _directory(client):
    root = Path(os.environ.get('LOCALAPPDATA') or os.environ.get('XDG_CACHE_HOME') or str(Path.home()/'.cache'))
    context = '\n'.join((str(Path(client.config['cache_path']).absolute()), client.config['base_url'], client.config['product_id'], client.config['device_id']))
    return root/'Licentivo'/'logout'/hashlib.sha256(context.encode()).hexdigest()

def _write(path, journal):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix='.logout-', dir=path.parent)
    try:
        os.chmod(name, 0o600)
        with os.fdopen(fd, 'w', encoding='utf-8') as stream:
            json.dump(journal, stream, separators=(',', ':'))
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)

def _save(client):
    failure = None
    journal = client.logout_journal
    journal['revision'] = max(journal.get('revision', 0)+1, int(time.time()*1000))
    try:
        scope = journal['source_hash']+'\n'+journal.get('binding_key', '')
        _write(_directory(client)/(hashlib.sha256(scope.encode()).hexdigest()+'.json'), journal)
    except OSError as error:
        failure = error
    try:
        _write(Path(client.config['cache_path'] + '.logout.json'), journal)
    except OSError:
        if failure:
            raise failure
    if failure and not journal['signed_out']:
        raise failure

def load(client):
    client.signed_out = False
    client.logout_bindings = set()
    client.logout_journal = dict(version=1, source_hash='', signed_out=False, pending=[])
    client.logout_thread = None
    records = {}
    def read(path):
        try:
            if path.stat().st_size > 65536:
                raise ValueError('invalid logout journal')
            journal = json.loads(path.read_bytes())
            if journal['version'] != 1 or not isinstance(journal['pending'], list):
                raise ValueError('invalid logout journal')
            scope = (journal['source_hash'], journal.get('binding_key', ''))
            old = records.get(scope)
            if old is None or (journal.get('revision', 0), bool(journal['signed_out'])) >= (old.get('revision', 0), bool(old['signed_out'])):
                records[scope] = journal
        except FileNotFoundError:
            pass
        except (OSError, ValueError, KeyError, TypeError):
            client.signed_out = True
    read(Path(client.config['cache_path'] + '.logout.json'))
    try:
        for path in _directory(client).glob('*.json'):
            read(path)
    except OSError:
        client.signed_out = True
    source, binding = client._hash(), _binding((client.auth or {}).get('logout_ticket'))
    pending = {}
    for journal in records.values():
        if journal['signed_out'] and journal.get('binding_key'):
            client.logout_bindings.add(journal['binding_key'])
        if (journal['source_hash'] == source or client.config.get('launch_connection_id') and binding and journal.get('binding_key') == binding or not client.auth and not client.config.get('identity_token') and not client.config['license_key']) and (not journal.get('binding_key') or not binding or journal['binding_key'] == binding):
            client.logout_journal = journal
            if journal['signed_out'] and not client.config['license_key']:
                client.signed_out = True
        for ticket in journal['pending']:
            pending[ticket['operation_id']] = ticket
    client.logout_journal['pending'] = list(pending.values())
    if client.signed_out:
        client.auth = None
    if client.logout_journal['pending']:
        recover(client)

def accept_login(client, ticket, error_type):
    if client.config.get('launch_connection_id') and _binding(ticket) in client.logout_bindings:
        if client.auth is not None:
            client.auth['logout_ticket'] = ticket
        deny(client)
        raise error_type(403, 'LAUNCH_LOGIN_REQUIRED')
    if client.logout_journal['signed_out']:
        client.logout_journal['signed_out'] = False
        _save(client)

def deny(client):
    client.signed_out = True
    if client.config['license_key'] or client.config.get('identity_token') or client.auth:
        client.logout_journal['source_hash'] = client._hash()
    client.logout_journal['signed_out'] = True
    ticket = (client.auth or {}).get('logout_ticket')
    if ticket:
        client.logout_journal['binding_key'] = _binding(ticket)
        client.logout_bindings.add(_binding(ticket))
    if ticket and not any(t['operation_id'] == ticket['operation_id'] for t in client.logout_journal['pending']):
        client.logout_journal['pending'].append(ticket)
    error = None
    try:
        _save(client)
    except OSError as failure:
        error = failure
    client._invalidate()
    client.stop.set()
    client.auth = None
    client.auth_dirty = False
    client.config['license_key'] = ''
    client.config['identity_token'] = ''
    if client.logout_journal['pending']:
        recover(client)
    return error

def retry(client):
    with client.lock:
        while client.logout_journal['pending']:
            ticket = client.logout_journal['pending'][0]
            request = urllib.request.Request(client.config['base_url'] + '/api/v1/sessions/logout', json.dumps(ticket).encode(), {'Content-Type': 'application/json'}, method='POST')
            with client.opener.open(request, timeout=min(client.timeout, 10)) as response:
                raw = response.read(65537)
                if response.status != 200 or len(raw) > 65536:
                    raise ValueError('device revocation pending')
            ack=json.loads(raw).get('data', {})
            if ack.get('status') not in ('succeeded', 'stale') or any(ack.get(k)!=ticket[k] for k in ('operation_id','activation_id','expected_generation')):
                raise ValueError('device revocation pending')
            client.logout_journal['pending'].pop(0)
            for path in _directory(client).glob('*.json'):
                if path.stat().st_size > 65536:
                    raise ValueError('invalid logout journal')
                journal = json.loads(path.read_bytes())
                remaining = [t for t in journal['pending'] if t['operation_id'] != ticket['operation_id']]
                if len(remaining) != len(journal['pending']):
                    journal['pending'] = remaining
                    _write(path, journal)
            _save(client)

def recover(client):
    if client.closed or client.logout_thread and client.logout_thread.is_alive():
        return
    def run():
        delay = 1
        while not client.closed:
            # Separate from the stopped heartbeat: logout must continue retrying.
            threading.Event().wait(delay)
            if client.closed:
                return
            try:
                retry(client)
                return
            except Exception:
                delay = min(delay * 2, 300)
    client.logout_thread = threading.Thread(target=run, daemon=True)
    client.logout_thread.start()

def logout(client):
    client.signed_out = True  # Deny before waiting for a network-holding mutex.
    with client.lock:
        error = deny(client)
        try:
            retry(client)
        except Exception:
            pass
        pending = bool(client.logout_journal['pending'])
        if pending:
            recover(client)
        result = dict(local_complete=error is None, revocation_pending=pending)
        if error:
            result['storage_error'] = 'LOCAL_CREDENTIAL_CLEANUP_FAILED'
        return result
