#!/usr/bin/env python3
import argparse,json,secrets,string,os,pathlib,re
p=argparse.ArgumentParser(description='Personalize a flashed F11 image boot partition')
p.add_argument('boot',help='mounted FAT boot partition')
p.add_argument('--hostname',default='f11-setup')
p.add_argument('--country',default='US')
p.add_argument('--timezone',default='America/New_York')
p.add_argument('--wifi-ssid')
p.add_argument('--wifi-password')
p.add_argument('--ssh-key')
a=p.parse_args(); boot=pathlib.Path(a.boot).resolve()
if not boot.is_dir() or not ((boot/'config.txt').exists() or (boot/'cmdline.txt').exists()): raise SystemExit('not a Raspberry Pi boot partition')
if not re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?',a.hostname): raise SystemExit('invalid hostname')
if bool(a.wifi_ssid)!=bool(a.wifi_password): raise SystemExit('Wi-Fi SSID and password must both be supplied')
if a.wifi_ssid and not (1<=len(a.wifi_ssid.encode())<=32 and 8<=len(a.wifi_password)<=63): raise SystemExit('invalid Wi-Fi settings')
alphabet='ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789'
device=secrets.token_hex(6); setup=''.join(secrets.choice(alphabet.upper()) for _ in range(10)); ap=''.join(secrets.choice(alphabet) for _ in range(16))
envelope={'schema':1,'device_id':device,'setup_code':setup,'ap_password':ap,'device':{'hostname':a.hostname,'country':a.country,'timezone':a.timezone}}
if a.wifi_ssid: envelope['wifi']={'ssid':a.wifi_ssid,'password':a.wifi_password}
if a.ssh_key:
 key=pathlib.Path(a.ssh_key).read_text().strip()
 if not re.fullmatch(r'(ssh-ed25519|ssh-rsa) [A-Za-z0-9+/=]+(?: .*)?',key): raise SystemExit('invalid SSH public key')
 envelope['ssh_authorized_key']=key
tmp=boot/'.f11-personalize.json.tmp'; final=boot/'f11-personalize.json'
fd=os.open(tmp,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
with os.fdopen(fd,'w') as f: json.dump(envelope,f,separators=(',',':')); f.write('\n'); f.flush(); os.fsync(f.fileno())
os.replace(tmp,final)
card=boot/'F11-SETUP.txt'; card.write_text(f'F11 Twitch Printer Setup\n========================\nSetup Wi-Fi: F11-SETUP-{device[-5:].upper()}\nWi-Fi password: {ap}\nSetup code: {setup}\nOpen: http://f11-setup.local:8080/\nFallback: http://10.42.0.1:8080/\nKeep this card private and delete it after setup.\n')
print(card)
