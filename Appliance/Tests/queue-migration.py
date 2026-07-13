#!/usr/bin/env python3
from pathlib import Path
import subprocess

helper=Path(__file__).resolve().parents[1]/"scripts/plan-queue-migration.py"
assert helper.exists(), "missing queue migration planner"

def run(discovery,current=""):
 p=subprocess.run(["python3",str(helper),current],input=discovery,text=True,capture_output=True)
 return p.returncode,p.stdout.strip(),p.stderr

def line(uri,key="MODEL",model="F11"):
 return f'direct {uri} "F11" "F11" "MANUFACTURER:;COMMAND SET:;{key}:{model};COMMENT:Impact Printer;" ""\n'

serial='usb:///F11?serial=ABC12345'
no_serial='usb://Rongta/F11'
for discovery in (line(serial,"MODEL"), line(serial,"MDL")):
 rc,out,_=run(discovery); assert rc==0 and out==serial
 rc,out,_=run(discovery,'device for Rongta_F11: f11:/'); assert rc==0 and out==serial
 rc,out,_=run(discovery,f'device for Rongta_F11: {serial}'); assert rc==0 and out==serial
rc,out,_=run(line(no_serial,"MDL"),f'device for Rongta_F11: {no_serial}'); assert rc==0 and out==no_serial
rc,_,_=run(line(serial),'device for Rongta_F11: socket://other'); assert rc!=0
rc,_,_=run(''); assert rc!=0
rc,_,_=run(line(serial)+line('usb:///F11?serial=XYZ67890')); assert rc!=0
rc,_,_=run(line(serial,"MDL","Other")); assert rc!=0
rc,_,_=run('direct usb:///F11?serial=A "unterminated\n'); assert rc!=0
print('queue migration: PASS')
