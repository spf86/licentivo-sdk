import json,sys
from licentivo import verify
with open(sys.argv[1] if len(sys.argv)>1 else 'tests/lease-vectors.json',encoding='utf-8') as f: fixture=json.load(f)
for v in fixture['vectors']:
    try: verify(v['envelope'],fixture['trusted_keys'],v['product_id'],v['device_id'],v['now']);ok=True
    except Exception: ok=False
    if ok!=v['valid']: raise AssertionError(v['name'])
print('Python:',len(fixture['vectors']),'lease vectors passed')
