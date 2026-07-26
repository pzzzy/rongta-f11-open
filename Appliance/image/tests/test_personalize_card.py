import json
import os
import pathlib
import re
import subprocess
import tempfile
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[2]
PERSONALIZER = ROOT / "image" / "personalize-card.py"
SETTINGS = ROOT / "image" / "settings.example.toml"


class PersonalizeCardTests(unittest.TestCase):
    def test_writes_only_bootfs_envelope_and_private_setup_card(self):
        with tempfile.TemporaryDirectory() as tmp:
            boot = pathlib.Path(tmp)
            (boot / "config.txt").write_text("# boot\n")
            before = {p.name for p in boot.iterdir()}
            completed = subprocess.run(
                [
                    "python3",
                    str(PERSONALIZER),
                    str(boot),
                    "--hostname",
                    "f11-setup",
                    "--country",
                    "US",
                    "--timezone",
                    "America/New_York",
                ],
                check=True,
                capture_output=True,
                text=True,
            )
            after = {p.name for p in boot.iterdir()}
            self.assertEqual(after - before, {"f11-personalize.json", "F11-SETUP.txt"})
            envelope_path = boot / "f11-personalize.json"
            envelope = json.loads(envelope_path.read_text())
            self.assertEqual(envelope["schema"], 1)
            self.assertRegex(envelope["device_id"], r"^[a-f0-9]{12}$")
            self.assertRegex(envelope["setup_code"], r"^[A-Z2-9]{10}$")
            self.assertEqual(len(envelope["ap_password"]), 16)
            self.assertNotIn("wifi", envelope)
            self.assertEqual(os.stat(envelope_path).st_mode & 0o777, 0o600)
            self.assertEqual(pathlib.Path(completed.stdout.strip()).resolve(), (boot / "F11-SETUP.txt").resolve())

    def test_settings_template_is_unpopulated_valid_toml(self):
        text = SETTINGS.read_text()
        self.assertTrue(all(not line.strip() or line.lstrip().startswith("#") for line in text.splitlines()))
        self.assertNotRegex(text, r"(?m)^(ssid|password|ssh_authorized_key)\s*=")
        self.assertNotIn("PRIVATE KEY", text)


if __name__ == "__main__":
    unittest.main()
