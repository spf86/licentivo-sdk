import argparse
import json
import time
from pathlib import Path
from licentivo import Client, APIError

parser = argparse.ArgumentParser()
parser.add_argument('--config', default='.local/protocol-fixture.json')
parser.add_argument('--policy-test',action='store_true')
parser.add_argument('--expect-denied',action='store_true')
parser.add_argument('--cache-test', action='store_true')
parser.add_argument('--refresh-test', action='store_true')
for option in ['improvements-test','startup-test','advanced-test','offline-export','offline-import','floating-test','transfer-test','transfer-import','logout-retry','logout-wait','metering-jobs-test','metering-features-test','metering-denial-test']: parser.add_argument('--'+option,action='store_true')
for option in ['metering-expired-cancel-test','metering-expired-commit-test','metering-recover-test','metering-boundary-test']: parser.add_argument('--'+option,action='store_true')
parser.add_argument('--offline-lifecycle-test',action='store_true')
args = parser.parse_args()
with open(args.config, encoding='utf-8') as f:
    cfg = json.load(f)

with Client(cfg) as client:
    if args.offline_lifecycle_test:
        phase=Path(args.config+'.phase').read_text()
        def export(suffix,operation): Path(args.config+suffix).write_text(json.dumps(client.offline_request(operation)),encoding='utf-8')
        def import_file(fresh=False):
            prefix='.fresh' if fresh else ''
            client.import_offline(json.loads(Path(args.config+prefix+'.request.json').read_text()),json.loads(Path(args.config+prefix+'.response.json').read_text()))
        def denied(callback):
            try: callback()
            except Exception: return
            raise AssertionError('operation unexpectedly restored access')
        def allowed(): assert client.check_feature('export') and client.status()['state']=='offline_valid', 'offline import did not publish offline access'
        if phase=='export': export('.request.json','activate')
        elif phase in ['import','retry']:
            import_file()
            if phase=='retry': import_file()
            allowed()
        elif phase=='fail_import':
            denied(import_file);assert not client.check_feature('export') and not client.status()['allowed']
        elif phase=='release':
            import_file();export('.release.json','deactivate');assert not client.check_feature('export')
        elif phase=='signedout':
            denied(client.activate);denied(lambda:client.offline_request('activate'));denied(import_file);assert not client.check_feature('export')
        elif phase=='logout':
            import_file();result=client.logout();assert result['local_complete'] and not result['revocation_pending'] and not client.check_feature('export')
        elif phase in ['old_export','renew_request']:
            if phase=='old_export': denied(import_file);assert not client.check_feature('export')
            export('.fresh.request.json','activate')
            if phase=='renew_request': denied(import_file);assert not client.check_feature('export')
        elif phase=='fresh_import':
            import_file(True);allowed();denied(import_file);allowed();import_file(True);allowed()
        else: raise AssertionError('unknown offline lifecycle phase')
        print('Python: offline lifecycle phase passed',phase);raise SystemExit(0)
    if args.logout_wait:
        assert client.check_feature('export');Path(args.config+'.ready').write_text('ready')
        deadline=time.monotonic()+30
        while not Path(args.config+'.continue').exists():
            if time.monotonic()>deadline: raise RuntimeError('logout wait timed out')
            time.sleep(.01)
        assert client.logout()['local_complete'] and not client.check_feature('export');raise SystemExit(0)
    if args.logout_retry:
        client.retry_pending_logouts();assert not client.check_feature('export');print('Python: pending logout completed');raise SystemExit(0)
    if args.metering_boundary_test:
        client.activate();calls = [0]
        def action(): calls[0] += 1
        if args.metering_recover_test:
            for _ in range(2): client.resolve_metered_feature('boundary-job', not args.expect_denied)
        failed = False
        try: client.run_metered_feature('export', 1, 'boundary-job', action)
        except Exception: failed = True
        expected_calls = 0 if args.metering_recover_test or args.expect_denied else 1
        assert failed == args.expect_denied and calls[0] == expected_calls, 'SDK executed outside authorization or repeated business'
        if args.metering_recover_test and args.expect_denied:
            client.run_metered_feature('export', 1, 'replacement-job', action)
            assert calls[0] == 1, 'cancellation did not release quota'
        print('Python: execution boundary regression passed');raise SystemExit(0)
    if args.metering_expired_cancel_test or args.metering_expired_commit_test:
        calls = [0]
        def action(): calls[0] += 1
        def expect_failure(callback):
            try: callback()
            except Exception: return
            raise AssertionError('expected operation failure')
        if not args.metering_recover_test:
            client.activate()
            def business():
                calls[0] += 1
                if args.metering_expired_cancel_test: raise RuntimeError('expected business failure')
            expect_failure(lambda: client.run_metered_feature('export', 1, 'expired-job', business))
            assert calls[0] == 1
        elif args.metering_expired_cancel_test:
            for _ in range(2): client.resolve_metered_feature('expired-job', False)
            expect_failure(lambda: client.resolve_metered_feature('expired-job', True))
            expect_failure(lambda: client.run_metered_feature('export', 1, 'expired-job', action))
            assert calls[0] == 0, 'canceled business repeated'
            client.run_metered_feature('export', 1, 'replacement-job', action)
            assert calls[0] == 1, 'expired cancellation did not release quota'
        else:
            expect_failure(lambda: client.run_metered_feature('export', 1, 'expired-job', action))
            expect_failure(lambda: client.resolve_metered_feature('expired-job', True))
            expect_failure(lambda: client.resolve_metered_feature('expired-job', False))
            assert calls[0] == 0, 'completed business repeated'
        print('Python: expired task regression passed');raise SystemExit(0)
    if args.metering_jobs_test or args.metering_features_test or args.metering_denial_test:
        client.activate()
        calls = [0]
        def action(): calls[0] += 1
        if args.metering_jobs_test:
            for job in ['ExportJob', 'exportjob', 'ExportJob']:
                client.run_metered_feature('export', 1, job, action)
            assert calls[0] == 2, 'distinct jobs were skipped or repeated'
        elif args.metering_features_test:
            for feature in ['export', 'Export']:
                assert client.consume(feature, 1, feature+'-job')['used'] == 1, 'feature meters were combined'
        else:
            client.reserve('export', 1, 'pending-job')
            failed = False
            try: client.run_metered_feature('export', 1, 'pending-job', action)
            except Exception: failed = True
            assert failed and calls[0] == 0, 'pending job bypassed current authorization'
        print('Python: metering regression passed');raise SystemExit(0)
    if args.improvements_test:
        client.start();one=client.reserve('export',1,'cancel-job')
        assert one['status']=='pending' and one['consumption']['used']==0 and one['consumption']['reserved']==1
        canceled=client.cancel(one['reservation_id'],'cancel-job');assert canceled['status']=='canceled' and canceled['consumption']['used']==0
        two=client.reserve('export',1,'commit-job')
        for _ in range(2):
            result=client.commit(two['reservation_id'],'commit-job');assert result['status']=='committed' and result['consumption']['used']==1
        assert client.reserve('export',1,'commit-job')['status']=='committed'
        try: client.consume('export',1000,'quota-job');raise AssertionError('quota granted')
        except APIError as e: assert e.code=='FEATURE_QUOTA_EXCEEDED' and e.request_id and not e.retryable
        assert client.status()['state']=='quota_exhausted' and client.check_feature('export')
        client.close();cfg['license_key']=''
        with Client(cfg) as restored:
            restored.start();restored.validate();assert restored.check_feature('export');restored.deactivate();assert not restored.status()['allowed']
        print('Python: credential / reservation / typed status OK');raise SystemExit(0)
    if args.startup_test:
        import time
        begin=time.monotonic();client.start();assert time.monotonic()-begin<.3
        time.sleep(.1);begin=time.monotonic();assert client.check_feature('export') and time.monotonic()-begin<.3
        print('Python: cached startup and local feature checks stay responsive');raise SystemExit(0)
    if args.offline_export:
        Path(args.config+'.request.json').write_text(json.dumps(client.offline_request()),encoding='utf-8')
        print('Python: offline request exported');raise SystemExit(0)
    if args.transfer_test or args.transfer_import:
        if args.transfer_import: client.import_offline(json.loads(Path(args.config+'.request.json').read_text()),json.loads(Path(args.config+'.response.json').read_text()))
        else: client.start()
        client.validate();client.heartbeat()
        if not client.check_feature('export'): raise RuntimeError('feature denied')
        print('Python: transferred device credential OK');raise SystemExit(0)
    if args.floating_test:
        client.start()
        with Client(cfg) as other:
            if other.check_feature('export'): raise RuntimeError('session cache reused')
            other.start()
            client.close();other.heartbeat()
            if not other.check_feature('export'): raise RuntimeError('new session denied')
        print('Python: floating seat release OK');raise SystemExit(0)
    if args.advanced_test or args.offline_import:
        if args.offline_import:
            client.import_offline(json.loads(Path(args.config+'.request.json').read_text()),json.loads(Path(args.config+'.response.json').read_text()))
        else: client.start()
        if not client.check_feature('export'): raise RuntimeError('feature denied')
        if client.consume('export',1,'business-job-1')['used']!=1 or client.consume('export',1,'business-job-1')['used']!=1: raise RuntimeError('consume replay failed')
        over=client.consume('export',3,'business-job-2')
        if over['used']!=4 or over['overage']!=1: raise RuntimeError('overage failed')
        failed=False
        try: client.consume('export',1,'business-job-3')
        except APIError as e: failed=e.code=='FEATURE_QUOTA_EXCEEDED'
        if not failed or not client.check_feature('export'): raise RuntimeError('quota denial failed')
        client.deactivate();print('Python: offline/metered feature checks OK');raise SystemExit(0)
    if args.refresh_test:
        failed=False
        try: client.refresh_online()
        except Exception: failed=True
        if not failed or not client.check_feature('export'): raise RuntimeError('refresh failure did not retain offline access')
        client.refresh_online()
        if not client.check_feature('updated') or client.check_feature('export'): raise RuntimeError('online refresh did not replace cached policy')
        failed=False
        try: client.refresh_online()
        except Exception: failed=True
        if not failed or client.check_feature('updated'): raise RuntimeError('refresh denial did not clear cache')
        print('Python: online refresh OK');raise SystemExit(0)
    if args.policy_test:
        if client.check_feature('export')==args.expect_denied: raise RuntimeError('unexpected cache device/policy access')
        if not args.expect_denied: client.activate();client.validate();client.heartbeat()
        print('Python: policy cache OK');raise SystemExit(0)
    claims = client.activate()
    print('Python: activated', claims['activation_id'])
    if not args.cache_test:
        client.validate()
        client.heartbeat()
        print('Python: export enabled', client.check_feature('export'))
        client.deactivate()
        print('Python: deactivated')

if args.cache_test:
    with Client(cfg) as restored:
        restored.validate()
        if not restored.check_feature('export'):
            raise RuntimeError('cache not restored during server outage')
        rejected = False
        try:
            restored.validate()
        except APIError as e:
            rejected = e.status == 403
        if not rejected or restored.check_feature('export'):
            raise RuntimeError('rejection did not invalidate cache')
    with Client(cfg) as reopened:
        if reopened.check_feature('export'):
            raise RuntimeError('rejected cache survived restart')
    print('Python: cache restored on 503; 403 invalidated access and persisted cache')
