#!/usr/bin/env python3
from pathlib import Path
import subprocess,tempfile
helper=Path(__file__).resolve().parents[1]/"scripts/check-f11-runtime.py"

def device(root,name,vid="0fe6",pid="811e",serial="ABC12345"):
 d=Path(root)/name; d.mkdir(); (d/"idVendor").write_text(vid); (d/"idProduct").write_text(pid)
 if serial is not None: (d/"serial").write_text(serial)

def run(line,root):
 return subprocess.run(["python3",str(helper),line,root],capture_output=True,text=True)
with tempfile.TemporaryDirectory() as root:
 device(root,"1-1")
 p=run("device for Rongta_F11: usb:///F11?serial=ABC12345",root); assert p.returncode==0,p.stderr
 p=run("device for Rongta_F11: usb://Rongta/F11",root); assert p.returncode==0,p.stderr
 assert run("device for Rongta_F11: socket://other",root).returncode!=0
 assert run("device for Other: usb:///F11?serial=ABC12345",root).returncode!=0
 assert run("device for Rongta_F11: usb:///F11?serial=WRONG",root).returncode!=0
 device(root,"1-2")
 assert run("device for Rongta_F11: usb://Rongta/F11",root).returncode!=0
print("runtime health: PASS")
