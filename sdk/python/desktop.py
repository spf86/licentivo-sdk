"""A desktop report exporter with real SDK feature gates."""
import json,queue,sys,threading,uuid
from pathlib import Path
import tkinter as tk
from tkinter import filedialog,ttk
from licentivo import Client

# BEGIN LICENTIVO PRODUCT
# Copy Product ID and all key ID/public key pairs from Console > Products > Signing public keys.
PRODUCT_ID = 'YOUR_PRODUCT_ID'
TRUSTED_KEYS = {'YOUR_KEY_ID': 'YOUR_PUBLIC_KEY'}
# END LICENTIVO PRODUCT

root=tk.Tk();root.title('Licentivo · Application example');root.geometry('650x360')
key=tk.StringVar();status=tk.StringVar(value='Activate once. Leave the key empty on later starts.')
metered=tk.BooleanVar();messages=queue.Queue();client=None;pending=None;busy=False
def work(action):
    global busy
    if busy:return
    busy=True
    def run():
        try:action()
        except Exception as e:messages.put(str(e))
        finally:messages.put(None)
    threading.Thread(target=run,daemon=True).start()
def connect():
    secret=key.get()
    def run():
        global client
        if pending:raise ValueError('Finish the pending commit before changing the client.')
        if client:client.close()
        client=None
        client=Client.open_product(PRODUCT_ID,TRUSTED_KEYS,secret,lambda e:messages.put(str(e))); client.on_status_changed=lambda s:messages.put(s['state']);messages.put(client.status()['state'])
    work(run)
def export():
    filename=filedialog.asksaveasfilename(defaultextension='.txt',initialfile='licensed-report.txt');limited=metered.get()
    if not filename:return
    def run():
        global pending
        if not client or not client.check_feature('export'):raise ValueError('Export is not permitted. Activate or refresh first.')
        journal=Path(client.config['cache_path']+'.export-job.json')
        if pending or journal.exists():raise ValueError('Finish the pending commit before exporting again.')
        job_id=str(uuid.uuid4());hold=client.reserve('export',1,job_id) if limited else None
        if hold and hold['status']!='pending':raise ValueError('This operation is closed. Do not export again.')
        if hold:journal.write_text(json.dumps({'reservation_id':hold['reservation_id'],'operation_id':job_id,'phase':'reserved','output':filename}),encoding='utf-8')
        try:client.run_feature('export',lambda:Path(filename).write_text('Item,Quantity,Price\nNotebook,2,12.50\nPen,5,3.00\n',encoding='utf-8'))
        except Exception:
            if hold:client.cancel(hold['reservation_id'],job_id);journal.unlink(missing_ok=True)
            raise
        if hold:
            pending=(hold['reservation_id'],job_id,journal);journal.write_text(json.dumps({'reservation_id':hold['reservation_id'],'operation_id':job_id,'phase':'exported','output':filename}),encoding='utf-8');finish()
        messages.put('Export completed: '+filename)
    work(run)
def finish():
    global pending
    if not client:return
    journal=Path(client.config['cache_path']+'.export-job.json')
    if pending is None and journal.exists():
        job=json.loads(journal.read_text(encoding='utf-8'))
        if job['phase']!='exported':raise ValueError('Interrupted export: inspect the output before completing this job.')
        pending=(job['reservation_id'],job['operation_id'],journal)
    if pending:
        rid,jid,journal=pending;result=client.commit(rid,jid);journal.unlink(missing_ok=True);pending=None;messages.put('Usage committed; remaining: '+str(result['consumption']['remaining']))
def poll():
    global busy
    while not messages.empty():
        item=messages.get()
        if item is None:busy=False
        else:status.set(item)
    root.after(100,poll)
frame=ttk.Frame(root,padding=24);frame.pack(fill='both',expand=True)
ttk.Label(frame,text='License key or temporary transfer code').pack(anchor='w');ttk.Entry(frame,textvariable=key,show='*').pack(fill='x',pady=5)
ttk.Button(frame,text='Activate / restore saved license',command=connect).pack(anchor='w',pady=8)
ttk.Checkbutton(frame,text='Export has a usage limit configured in the policy',variable=metered).pack(anchor='w')
buttons=ttk.Frame(frame);buttons.pack(fill='x',pady=8)
ttk.Button(buttons,text='Export a report',command=export).pack(side='left');ttk.Button(buttons,text='Retry pending commit',command=lambda:work(finish)).pack(side='left',padx=6)
ttk.Button(buttons,text='Release this device',command=lambda:work(lambda:client.deactivate() if client else None)).pack(side='left')
ttk.Button(buttons,text='Pro export',command=lambda:work(lambda:client.run_feature('pro',lambda:Path('pro-report.csv').write_text('Pro report\n',encoding='utf-8')) if client else None)).pack(side='left')
ttk.Button(frame,text='Refresh license',command=lambda:work(lambda:client.refresh_online() if client else None)).pack(anchor='w')
ttk.Label(frame,textvariable=status,wraplength=590).pack(fill='x',pady=12)
root.after(100,poll);root.mainloop()
if client:client.close()
