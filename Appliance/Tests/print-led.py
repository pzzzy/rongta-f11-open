#!/usr/bin/env python3
import os
import pathlib
import subprocess
import tempfile
import time

root = pathlib.Path(__file__).resolve().parents[1]
daemon = root / "image/rootfs/usr/local/lib/f11/print-led"
with tempfile.TemporaryDirectory() as td:
    td = pathlib.Path(td)
    led = td / "ACT"
    markers = td / "markers"
    state = td / "state"
    led.mkdir()
    markers.mkdir()
    state.write_text("idle\n")
    lpstat = td / "lpstat"
    lpstat.write_text("#!/bin/bash\n[[ $1 == -p && $2 == Rongta_F11_Media ]] || exit 2\nif [[ $(cat '" + str(state) + "') == printing ]]; then echo 'printer Rongta_F11_Media is now printing Rongta_F11_Media-1. enabled since now'; else echo 'printer Rongta_F11_Media is idle. enabled since now'; fi\n")
    lpstat.chmod(0o755)
    for name, value in {"trigger": "actpwr\n", "brightness": "255\n", "delay_on": "0\n", "delay_off": "0\n"}.items():
        (led / name).write_text(value)
    env = os.environ | {"F11_LED_ROOT": str(led), "F11_PRINT_MARKER_DIR": str(markers), "F11_LPSTAT": str(lpstat), "F11_LED_INTERVAL": "0.05"}
    proc = subprocess.Popen([str(daemon)], env=env)
    worker = None
    try:
        time.sleep(0.15)
        assert (led / "trigger").read_text().strip() == "none"
        assert (led / "brightness").read_text().strip() == "0"
        stale = markers / "job-999999"
        stale.touch()
        time.sleep(0.15)
        assert not stale.exists()
        worker = subprocess.Popen(["/bin/sleep", "10"])
        (markers / f"job-{worker.pid}").touch()
        time.sleep(0.15)
        assert (led / "trigger").read_text().strip() == "timer"
        assert (led / "delay_on").read_text().strip() == "180"
        assert (led / "delay_off").read_text().strip() == "180"
        state.write_text("printing\n")
        worker.terminate()
        worker.wait(timeout=3)
        worker = None
        time.sleep(0.15)
        assert (led / "trigger").read_text().strip() == "timer"
        state.write_text("idle\n")
        time.sleep(0.15)
        assert (led / "trigger").read_text().strip() == "none"
        assert (led / "brightness").read_text().strip() == "0"
    finally:
        if worker is not None:
            worker.terminate()
            worker.wait(timeout=3)
        proc.terminate()
        proc.wait(timeout=3)
    assert (led / "trigger").read_text().strip() == "none"
    assert (led / "brightness").read_text().strip() == "0"
print("print LED: PASS")
