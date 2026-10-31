"""Private task journal. It contains IDs/outcome only, never customer business data."""
import hashlib
import json
import os
import tempfile
import threading
from contextlib import contextmanager
from datetime import datetime, timezone
from pathlib import Path


_lock_fds = set()


def _after_fork():
    # Closing the child's descriptors does not unlock the parent's open file
    # description. Never let an idle child retain a dead parent's task lock.
    for fd in _lock_fds:
        os.close(fd)
    _lock_fds.clear()


if hasattr(os, 'register_at_fork'):
    os.register_at_fork(after_in_child=_after_fork)


def _check_process(client):
    if client.feature_task_pid != os.getpid():
        raise RuntimeError('client cannot be used after fork; create a new Client in the child process')


def _paths(client, operation_id):
    if not isinstance(operation_id, str) or not 1 <= len(operation_id) <= 80 or any(ord(char) < 33 or ord(char) > 126 for char in operation_id):
        raise ValueError('a stable operation_id of 1-80 non-whitespace ASCII characters is required')
    source = client._hash()
    legacy = Path(client.config['cache_path'] + '.task-' + hashlib.sha256((source + '\n' + operation_id).encode()).hexdigest() + '.json')
    if client.config.get('launch_connection_id'):
        with client.lock:
            claims = client._check()
        source = hashlib.sha256(('LicentivoTask/v2\n' + client.config['base_url'] + '\n' + claims['product_id'] + '\n' + claims['license_id'] + '\n' + claims['device_id']).encode()).hexdigest()
    digest = hashlib.sha256((source + '\n' + operation_id).encode()).hexdigest()
    path = Path(client.config['cache_path'] + '.task-' + digest + '.json')
    return path, legacy


@contextmanager
def _locked_task(client, operation_id):
    _check_process(client)
    if not client.feature_task_lock.acquire(blocking=False):
        raise RuntimeError('another feature task is running')
    fd = None
    locked = False
    try:
        path, legacy = _paths(client, operation_id)
        path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        # Keep this inode after unlock: unlinking a lock file would let a new
        # process lock a different inode while another still owns the old one.
        fd = os.open(str(path) + '.lock', os.O_CREAT | os.O_RDWR, 0o600)
        _lock_fds.add(fd)
        try:
            if os.name == 'nt':
                import msvcrt
                msvcrt.locking(fd, msvcrt.LK_NBLCK, 1)
            else:
                import fcntl
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            locked = True
        except OSError as error:
            raise RuntimeError('another process is running this feature task') from error
        # Migration and every read/write/settlement share the same process lock.
        if path != legacy and not path.exists() and legacy.exists():
            legacy.rename(path)
        yield path
    finally:
        # An action may itself fork. The child must never unlock the parent's
        # reservation or release descriptors already closed by the fork hook.
        if client.feature_task_pid == os.getpid():
            try:
                if fd is not None:
                    try:
                        if locked:
                            if os.name == 'nt':
                                import msvcrt
                                msvcrt.locking(fd, msvcrt.LK_UNLCK, 1)
                            else:
                                import fcntl
                                fcntl.flock(fd, fcntl.LOCK_UN)
                    finally:
                        _lock_fds.discard(fd)
                        os.close(fd)
            finally:
                client.feature_task_lock.release()


def _deadline(hold):
    deadline = datetime.fromisoformat(hold['expires_at'].replace('Z', '+00:00'))
    if deadline.tzinfo is None:
        raise ValueError('reservation expiry must include a timezone')
    return deadline


@contextmanager
def _renewing(client, task, hold):
    stop = threading.Event()
    deadline = _deadline(hold)

    def renew():
        until, interval = deadline, 60
        while True:
            remaining = (until - datetime.now(timezone.utc)).total_seconds()
            if remaining <= 0 or stop.wait(min(interval, max(0.01, remaining / 3))):
                return
            try:
                renewed = client.reserve(task['feature'], task['quantity'], task['operation_id'])
                if renewed['status'] != 'pending' or renewed['reservation_id'] != task['reservation_id']:
                    return
                until = _deadline(renewed)
                interval = 60
            except Exception:
                # The business callback is already running. Preserve its actual
                # outcome, retry before expiry, and let commit report settlement.
                interval = 5

    worker = threading.Thread(target=renew, name='licentivo-reservation', daemon=True)
    worker.start()
    try:
        yield
    finally:
        if client.feature_task_pid == os.getpid():
            stop.set()
            worker.join()


def _read(path, operation_id):
    if not path.exists():
        return dict(version=1, operation_id=operation_id, reservation_id='', state='')
    raw = path.read_bytes()
    task = json.loads(raw) if len(raw) <= 4096 else None
    if not isinstance(task, dict) or task.get('version') != 1 or task.get('operation_id') != operation_id or not isinstance(task.get('feature'), str) or not task['feature'] or not isinstance(task.get('quantity'), int) or task['quantity'] <= 0 or not isinstance(task.get('state'), str) or not isinstance(task.get('reservation_id'), str) or not task['reservation_id']:
        raise ValueError('invalid feature task journal; do not repeat business')
    return task


def _save(path, task):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    fd, temporary = tempfile.mkstemp(prefix='.feature-task-', dir=path.parent)
    try:
        with os.fdopen(fd, 'w', encoding='utf-8') as stream:
            json.dump(task, stream)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


def _finish(client, path, task, succeeded):
    try:
        result = (client.commit if succeeded else client.cancel)(task['reservation_id'], task['operation_id'])
    except Exception as error:
        raise RuntimeError(f"confirmation pending for operation {task['operation_id']}; do not repeat business") from error
    expected = 'committed' if succeeded else 'canceled'
    if result['status'] != expected and (succeeded or result['status'] != 'expired'):
        raise RuntimeError('reservation closed without the requested outcome; do not repeat business')
    task['state'] = expected
    _save(path, task)


def run(client, feature, quantity, operation_id, action):
    if not callable(action) or not isinstance(quantity, int) or isinstance(quantity, bool) or quantity <= 0:
        raise ValueError('business action and positive integer quantity are required')
    with _locked_task(client, operation_id) as path:
        task = _read(path, operation_id)
        state = task['state']
        if state and (task['feature'] != feature or task['quantity'] != quantity):
            raise ValueError('operation ID was already used for another feature or quantity')
        task.update(feature=feature, quantity=quantity)
        if state == 'committed': return
        if state == 'commit_pending': return _finish(client, path, task, True)
        if state == 'cancel_pending': _finish(client, path, task, False); state = 'canceled'
        if state == 'canceled': raise RuntimeError('operation canceled; use a new job ID for a new action')
        if state == 'business_running': raise RuntimeError('business outcome unknown; verify its result and call resolve_metered_feature')
        if state: raise ValueError('invalid feature task state')
        client.run_feature(feature, lambda: None)
        hold = client.reserve(feature, quantity, operation_id)
        if hold['status'] == 'committed':
            task.update(reservation_id=hold['reservation_id'], state='committed')
            _save(path, task)
            return
        if hold['status'] != 'pending': raise RuntimeError('reservation is closed; business was not run')
        task.update(reservation_id=hold['reservation_id'], state='business_running')
        try: _save(path, task)
        except Exception:
            try: client.cancel(hold['reservation_id'], operation_id)
            except Exception: pass
            raise
        def authorized_action():
            if _deadline(hold) <= datetime.now(timezone.utc):
                raise RuntimeError('reservation expired; business was not run')
            with _renewing(client, task, hold):
                return action()
        try:
            client.run_feature(feature, authorized_action)
            _check_process(client)
        except Exception:
            _check_process(client)
            task['state'] = 'cancel_pending'
            try: _save(path, task); _finish(client, path, task, False)
            except Exception: pass  # The original business error remains authoritative.
            raise
        task['state'] = 'commit_pending'
        try: _save(path, task)
        except Exception as error:
            raise RuntimeError(f'business completed; journal update failed; do not repeat operation {operation_id}') from error
        _finish(client, path, task, True)


def resolve(client, operation_id, succeeded):
    with _locked_task(client, operation_id) as path:
        task = _read(path, operation_id)
        state = task['state']
        if state == ('committed' if succeeded else 'canceled'): return
        if not isinstance(succeeded, bool) or not task['reservation_id'] or state not in ('business_running', 'commit_pending', 'cancel_pending') or state == 'commit_pending' and not succeeded or state == 'cancel_pending' and succeeded:
            raise RuntimeError('task cannot be resolved with that outcome')
        task['state'] = 'commit_pending' if succeeded else 'cancel_pending'
        _save(path, task)
        _finish(client, path, task, succeeded)
