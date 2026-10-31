"""Regression tests for metered callbacks, shared journals and reservation renewal."""
import multiprocessing
import os
import tempfile
import threading
import time
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

import licentivo_tasks as tasks


class TaskClient:
    def __init__(self, directory, ttl=30):
        self.config = {'cache_path': str(Path(directory) / 'lease.json')}
        self.feature_task_lock = threading.Lock()
        self.feature_task_pid = os.getpid()
        self.ttl = ttl
        self.hold = None
        self.reserves = self.commits = self.cancels = 0

    def _hash(self):
        return 'test-license'

    def run_feature(self, feature, action):
        return action()

    def reserve(self, feature, quantity, operation_id):
        self.reserves += 1
        now = datetime.now(timezone.utc)
        if self.hold and datetime.fromisoformat(self.hold['expires_at']) <= now:
            return dict(self.hold, status='expired')
        self.hold = dict(reservation_id='test-reservation', operation_id=operation_id,
                         status='pending', expires_at=(now + timedelta(seconds=self.ttl)).isoformat())
        return dict(self.hold)

    def commit(self, reservation_id, operation_id):
        if datetime.fromisoformat(self.hold['expires_at']) <= datetime.now(timezone.utc):
            raise RuntimeError('RESERVATION_EXPIRED')
        self.commits += 1
        return dict(self.hold, status='committed')

    def cancel(self, reservation_id, operation_id):
        self.cancels += 1
        return dict(self.hold, status='canceled')


def shared_worker(directory, first, reserved, release, results):
    client = TaskClient(directory)
    if first:
        original = client.reserve

        def wait_before_journal(*args):
            reserved.set()
            if not release.wait(15):
                raise RuntimeError('test did not release reservation response')
            return original(*args)

        client.reserve = wait_before_journal

    def action():
        with open(Path(directory) / 'business.txt', 'ab', buffering=0) as stream:
            stream.write(b'executed\n')

    try:
        tasks.run(client, 'export', 1, 'shared-job', action)
        results.put(('ok', client.commits))
    except RuntimeError as error:
        results.put(('blocked', str(error)))


class FeatureTaskTests(unittest.TestCase):
    def test_processes_cannot_both_run_before_journal_is_saved(self):
        # Force the fork race's exact window: both clients can see an empty
        # journal while the first reserve response has not yet been saved.
        with tempfile.TemporaryDirectory() as directory:
            context = multiprocessing.get_context('spawn')
            reserved, release = context.Event(), context.Event()
            results = context.Queue()
            first = context.Process(target=shared_worker, args=(directory, True, reserved, release, results))
            second = context.Process(target=shared_worker, args=(directory, False, reserved, release, results))
            first.start()
            try:
                self.assertTrue(reserved.wait(10), 'first process did not reserve')
                second.start()
                second.join(10)
                self.assertFalse(second.is_alive(), 'concurrent call must fail without blocking')
                release.set()
                first.join(10)
                self.assertEqual(first.exitcode, 0)
                self.assertEqual(second.exitcode, 0)
                self.assertEqual((Path(directory) / 'business.txt').read_text().splitlines(), ['executed'])
                outcomes = [results.get(timeout=2), results.get(timeout=2)]
                self.assertEqual(sum(item == ('ok', 1) for item in outcomes), 1)
                self.assertEqual(sum(item[0] == 'blocked' for item in outcomes), 1)
            finally:
                release.set()
                for process in (first, second):
                    if process.pid is not None:
                        process.join(2)
                        if process.is_alive():
                            process.terminate()
                            process.join(2)
                results.close()

    def test_inherited_client_is_rejected_before_taking_thread_lock(self):
        with tempfile.TemporaryDirectory() as directory:
            client = TaskClient(directory)
            client.feature_task_pid = -1
            client.feature_task_lock.acquire()
            for call in (lambda: tasks.run(client, 'export', 1, 'fork-job', lambda: self.fail('business ran')),
                         lambda: tasks.resolve(client, 'fork-job', True)):
                with self.assertRaisesRegex(RuntimeError, 'fork'):
                    call()
            self.assertEqual(client.reserves, 0)

    @unittest.skipUnless(hasattr(os, 'fork'), 'requires POSIX fork')
    def test_forked_child_cannot_reuse_parent_client(self):
        with tempfile.TemporaryDirectory() as directory:
            client = TaskClient(directory)
            client.feature_task_lock.acquire()
            child = os.fork()
            if child == 0:
                try:
                    tasks.run(client, 'export', 1, 'fork-job', lambda: None)
                except RuntimeError as error:
                    os._exit(0 if 'fork' in str(error) else 1)
                os._exit(2)
            _, status = os.waitpid(child, 0)
            self.assertEqual(os.waitstatus_to_exitcode(status), 0)

    def test_dead_process_does_not_leave_task_locked(self):
        with tempfile.TemporaryDirectory() as directory:
            context = multiprocessing.get_context('spawn')
            reserved, release = context.Event(), context.Event()
            results = context.Queue()
            process = context.Process(target=shared_worker, args=(directory, True, reserved, release, results))
            process.start()
            try:
                self.assertTrue(reserved.wait(10))
            finally:
                process.terminate()
                process.join(10)
                results.close()
            client = TaskClient(directory)
            calls = []
            tasks.run(client, 'export', 1, 'shared-job', lambda: calls.append('executed'))
            self.assertEqual(calls, ['executed'])
            self.assertEqual(client.commits, 1)

    @unittest.skipUnless(hasattr(os, 'fork'), 'requires POSIX fork')
    def test_child_closing_inherited_descriptor_keeps_parent_lock(self):
        with tempfile.TemporaryDirectory() as directory:
            client = TaskClient(directory)
            with tasks._locked_task(client, 'fork-job'):
                child = os.fork()
                if child == 0:
                    try:
                        tasks.run(TaskClient(directory), 'export', 1, 'fork-job', lambda: None)
                    except RuntimeError as error:
                        os._exit(0 if 'another process' in str(error) else 1)
                    os._exit(2)
                _, status = os.waitpid(child, 0)
                self.assertEqual(os.waitstatus_to_exitcode(status), 0)
            calls = []
            tasks.run(client, 'export', 1, 'fork-job', lambda: calls.append('executed'))
            self.assertEqual(calls, ['executed'])

    def test_long_business_renews_and_commits_without_reexecution(self):
        with tempfile.TemporaryDirectory() as directory:
            client = TaskClient(directory, ttl=0.18)
            calls = []

            def action():
                calls.append('executed')
                time.sleep(0.6)

            tasks.run(client, 'export', 1, 'long-job', action)
            tasks.run(client, 'export', 1, 'long-job', action)
            tasks.resolve(client, 'long-job', True)
            self.assertEqual(calls, ['executed'])
            self.assertGreater(client.reserves, 2)
            self.assertEqual(client.commits, 1)
            self.assertEqual(client.cancels, 0)

    def test_failed_business_stops_renewal_and_preserves_error(self):
        with tempfile.TemporaryDirectory() as directory:
            client = TaskClient(directory, ttl=0.18)

            def action():
                time.sleep(0.3)
                raise ValueError('business failed')

            with self.assertRaisesRegex(ValueError, 'business failed'):
                tasks.run(client, 'export', 1, 'failed-job', action)
            reserves = client.reserves
            time.sleep(0.2)
            self.assertEqual(client.reserves, reserves, 'renewal thread leaked')
            self.assertEqual(client.cancels, 1)
            self.assertEqual(client.commits, 0)

    def test_resolution_cannot_cancel_another_clients_running_business(self):
        with tempfile.TemporaryDirectory() as directory:
            client, other = TaskClient(directory), TaskClient(directory)

            def action():
                with self.assertRaisesRegex(RuntimeError, 'another process'):
                    tasks.resolve(other, 'running-job', False)

            tasks.run(client, 'export', 1, 'running-job', action)
            self.assertEqual(client.commits, 1)
            self.assertEqual(other.cancels, 0)

    def test_lost_renewals_keep_success_pending_without_reexecution(self):
        with tempfile.TemporaryDirectory() as directory:
            client = TaskClient(directory, ttl=0.12)
            original = client.reserve

            def reserve_once(*args):
                if client.hold:
                    raise OSError('offline')
                return original(*args)

            client.reserve = reserve_once
            calls = []

            def action():
                calls.append('executed')
                time.sleep(0.25)

            with self.assertRaisesRegex(RuntimeError, 'confirmation pending'):
                tasks.run(client, 'export', 1, 'offline-job', action)
            with self.assertRaisesRegex(RuntimeError, 'confirmation pending'):
                tasks.run(client, 'export', 1, 'offline-job', action)
            self.assertEqual(calls, ['executed'])
            self.assertEqual(client.cancels, 0)

    def test_transient_renewal_error_recovers_after_original_deadline(self):
        with tempfile.TemporaryDirectory() as directory:
            client = TaskClient(directory, ttl=0.3)
            original = client.reserve
            attempts = []

            def intermittent(*args):
                attempts.append(True)
                if len(attempts) == 3:
                    raise OSError('temporary network error')
                return original(*args)

            client.reserve = intermittent
            tasks.run(client, 'export', 1, 'transient-job', lambda: time.sleep(0.9))
            self.assertGreater(len(attempts), 4)
            self.assertEqual(client.commits, 1)
            self.assertEqual(client.cancels, 0)


if __name__ == '__main__':
    unittest.main()
