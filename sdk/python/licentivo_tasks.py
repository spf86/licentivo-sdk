"""Private task journal. It contains IDs/outcome only, never customer business data."""
import hashlib
import json
import os
import tempfile
from datetime import datetime, timezone
from pathlib import Path


def _path(client, operation_id):
    if not isinstance(operation_id, str) or not 1 <= len(operation_id) <= 80 or any(ord(char) < 33 or ord(char) > 126 for char in operation_id):
        raise ValueError('a stable operation_id of 1-80 non-whitespace ASCII characters is required')
    digest = hashlib.sha256((client._hash() + '\n' + operation_id).encode()).hexdigest()
    return Path(client.config['cache_path'] + '.task-' + digest + '.json')


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
    if not client.feature_task_lock.acquire(blocking=False):
        raise RuntimeError('another feature task is running')
    try:
        path = _path(client, operation_id)
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
            deadline = datetime.fromisoformat(hold['expires_at'].replace('Z', '+00:00'))
            if deadline.tzinfo is None or deadline <= datetime.now(timezone.utc):
                raise RuntimeError('reservation expired; business was not run')
            return action()
        try: client.run_feature(feature, authorized_action)
        except Exception:
            task['state'] = 'cancel_pending'
            try: _save(path, task); _finish(client, path, task, False)
            except Exception: pass  # The original business error remains authoritative.
            raise
        task['state'] = 'commit_pending'
        try: _save(path, task)
        except Exception as error:
            raise RuntimeError(f'business completed; journal update failed; do not repeat operation {operation_id}') from error
        _finish(client, path, task, True)
    finally:
        client.feature_task_lock.release()


def resolve(client, operation_id, succeeded):
    if not client.feature_task_lock.acquire(blocking=False): raise RuntimeError('another feature task is running')
    try:
        path = _path(client, operation_id)
        task = _read(path, operation_id)
        state = task['state']
        if state == ('committed' if succeeded else 'canceled'): return
        if not isinstance(succeeded, bool) or not task['reservation_id'] or state not in ('business_running', 'commit_pending', 'cancel_pending') or state == 'commit_pending' and not succeeded or state == 'cancel_pending' and succeeded:
            raise RuntimeError('task cannot be resolved with that outcome')
        task['state'] = 'commit_pending' if succeeded else 'cancel_pending'
        _save(path, task)
        _finish(client, path, task, succeeded)
    finally:
        client.feature_task_lock.release()
