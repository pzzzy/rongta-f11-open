#!/usr/bin/env python3
import builtins
import contextlib
import dataclasses
import hashlib
import importlib.util
import io
import json
import lzma
import pathlib
import tempfile
import unittest
from unittest import mock

SCRIPT = pathlib.Path(__file__).resolve().parents[1] / "flash-card.py"
spec = importlib.util.spec_from_file_location("flash_card", SCRIPT)
flash = importlib.util.module_from_spec(spec)
spec.loader.exec_module(flash)


def disk(**changes):
    values = dict(
        identifier="disk9", path="/dev/disk9", raw_path="/dev/rdisk9",
        model="Test SD Card", size=32 * 1024**3, protocol="USB",
        internal=False, whole=True, writable=True, virtual=False,
        removable=True, ejectable=True,
        mounts=("/Volumes/TEST",),
        media_identity=("test-media-uuid",),
        physical_identity=("IODeviceTree:/test/reader",),
    )
    values.update(changes)
    return flash.Disk(**values)


class FlashCardTests(unittest.TestCase):
    def make_image(self, directory, payload=b"safe-image" * 1000):
        image = pathlib.Path(directory) / "f11-twitch-zero-test.img.xz"
        image.write_bytes(lzma.compress(payload))
        digest = hashlib.sha256(image.read_bytes()).hexdigest()
        image.with_name(image.name + ".sha256").write_text(f"{digest}  {image.name}\n")
        return image, payload

    def test_whole_identifier_rejects_partition_like_or_arbitrary_paths(self):
        self.assertEqual(flash.whole_identifier("/dev/disk4"), "disk4")
        self.assertEqual(flash.whole_identifier("disk4s2"), "disk4")
        self.assertEqual(flash.explicit_whole_identifier("/dev/disk4"), "disk4")
        self.assertEqual(flash.explicit_whole_identifier("disk4s2"), "")
        self.assertEqual(flash.whole_identifier("/dev/nvme0n1"), "")
        self.assertEqual(flash.whole_identifier("/tmp/file"), "")

    def test_read_disk_accepts_current_macos_writable_media_schema(self):
        info = {
            "DeviceIdentifier": "disk9", "MediaName": "SD Reader", "TotalSize": 32 * 1024**3,
            "BusProtocol": "USB", "Internal": False, "WritableMedia": True,
            "VirtualOrPhysical": "Physical", "DeviceTreePath": "IODeviceTree:/usb/test",
            "MediaUUID": "test-media-uuid", "DiskUUID": "clonable-disk-uuid",
        }
        with mock.patch.object(flash, "diskutil_plist", return_value=info):
            parsed = flash.read_disk("disk9", listed_whole=True)
        self.assertTrue(parsed.whole)
        self.assertTrue(parsed.writable)
        self.assertEqual(parsed.media_identity, ("test-media-uuid",))
        self.assertEqual(parsed.physical_identity, ("IODeviceTree:/usb/test",))

    def test_read_disk_rejects_mismatched_diskutil_identifier(self):
        with mock.patch.object(flash, "diskutil_plist", return_value={"DeviceIdentifier": "disk8"}):
            with self.assertRaises(flash.FlashError):
                flash.read_disk("disk9", listed_whole=True)

    def test_disk_uuid_without_media_uuid_fails_closed(self):
        info = {
            "DeviceIdentifier": "disk9", "TotalSize": 32 * 1024**3, "Internal": False,
            "WritableMedia": True, "VirtualOrPhysical": "Physical", "DiskUUID": "cloned-uuid",
        }
        with mock.patch.object(flash, "diskutil_plist", return_value=info):
            parsed = flash.read_disk("disk9", listed_whole=True)
        self.assertEqual(parsed.media_identity, ())
        with self.assertRaises(flash.FlashError):
            flash.validate_destination(parsed, 4096, "disk3")

    def test_builtin_removable_sd_is_allowed_but_internal_drive_is_not(self):
        card = disk(
            internal=True, protocol="Secure Digital", removable=True, ejectable=True,
            media_identity=("sd-serial:0x1234",),
            physical_identity=("IODeviceTree:/builtin/sdreader",),
        )
        flash.validate_destination(card, 4096, "disk3")
        with self.assertRaises(flash.FlashError):
            flash.validate_destination(dataclasses.replace(card, protocol="USB"), 4096, "disk3")
        with self.assertRaises(flash.FlashError):
            flash.validate_destination(dataclasses.replace(card, removable=False), 4096, "disk3")
        with self.assertRaises(flash.FlashError):
            flash.validate_destination(dataclasses.replace(card, media_identity=("uuid",)), 4096, "disk3")

    def test_explicit_device_must_be_in_external_physical_listing(self):
        with tempfile.TemporaryDirectory() as tmp:
            image, _ = self.make_image(tmp)
            with mock.patch.object(flash.sys, "platform", "darwin"), \
                 mock.patch.object(flash, "list_external_disks", return_value=[]), \
                 mock.patch.object(flash, "external_physical_identifiers", return_value=set()), \
                 mock.patch.object(flash, "read_disk") as reader:
                with self.assertRaises(flash.FlashError):
                    flash.main(["--image", str(image), "--device", "/dev/disk9", "--dry-run"])
                reader.assert_not_called()

    def test_partition_device_argument_is_rejected_before_disk_lookup(self):
        with tempfile.TemporaryDirectory() as tmp:
            image, _ = self.make_image(tmp)
            with mock.patch.object(flash.sys, "platform", "darwin"), \
                 mock.patch.object(flash, "list_external_disks", return_value=[]), \
                 mock.patch.object(flash, "read_disk") as reader:
                with self.assertRaises(flash.FlashError):
                    flash.main(["--image", str(image), "--device", "/dev/disk9s1", "--dry-run"])
                reader.assert_not_called()

    def test_validate_rejects_system_internal_virtual_readonly_and_small_disks(self):
        cases = [
            disk(identifier="disk3", path="/dev/disk3", raw_path="/dev/rdisk3"),
            disk(internal=True), disk(virtual=True), disk(writable=False),
            disk(size=1024), disk(whole=False),
            disk(media_identity=()), disk(physical_identity=()),
        ]
        for candidate in cases:
            with self.subTest(candidate=candidate):
                with self.assertRaises(flash.FlashError):
                    flash.validate_destination(candidate, 4096, "disk3")

    def test_checksum_mismatch_stops_before_device_discovery(self):
        with tempfile.TemporaryDirectory() as tmp:
            image, _ = self.make_image(tmp)
            image.with_name(image.name + ".sha256").write_text("0" * 64 + f"  {image.name}\n")
            with mock.patch.object(flash, "list_external_disks") as listing:
                with self.assertRaises(flash.FlashError):
                    flash.main(["--image", str(image), "--dry-run"])
                listing.assert_not_called()

    def test_release_directory_default_image_discovery(self):
        with tempfile.TemporaryDirectory() as tmp:
            release = pathlib.Path(tmp)
            image, _ = self.make_image(release)
            with mock.patch.object(flash, "__file__", str(release / "flash-card.py")):
                self.assertEqual(flash.default_image(), image.resolve())

    def test_confirmation_requires_exact_device_and_erase_phrase(self):
        candidate = disk()
        for answers in (["/dev/disk8"], ["/dev/disk9", "ERASE /dev/disk8"]):
            with mock.patch.object(builtins, "input", side_effect=answers):
                with self.assertRaises(flash.FlashError):
                    flash.confirm_erase(candidate)
        with mock.patch.object(builtins, "input", side_effect=["/dev/disk9", "ERASE /dev/disk9"]):
            flash.confirm_erase(candidate)

    def test_dry_run_never_unmounts_writes_or_ejects(self):
        with tempfile.TemporaryDirectory() as tmp:
            image, payload = self.make_image(tmp)
            candidate = disk(size=len(payload) + 1024)
            with mock.patch.object(flash.sys, "platform", "darwin"), \
                 mock.patch.object(flash, "list_external_disks", return_value=[candidate]), \
                 mock.patch.object(flash, "choose_disk", return_value=candidate), \
                 mock.patch.object(flash, "read_disk", return_value=candidate), \
                 mock.patch.object(flash, "root_disk_identifier", return_value="disk3"), \
                 mock.patch.object(flash, "confirm_erase"), \
                 mock.patch.object(flash, "flash_image") as writer, \
                 mock.patch.object(flash, "verify_written") as verifier, \
                 mock.patch.object(flash, "run") as command:
                self.assertEqual(flash.main(["--image", str(image), "--dry-run"]), 0)
                writer.assert_not_called()
                verifier.assert_not_called()
                command.assert_not_called()

    def test_device_change_after_confirmation_is_rejected_before_write(self):
        with tempfile.TemporaryDirectory() as tmp:
            image, payload = self.make_image(tmp)
            selected = disk(size=len(payload) + 1024)
            changed = disk(size=len(payload) + 2048)
            with mock.patch.object(flash.sys, "platform", "darwin"), \
                 mock.patch.object(flash, "list_external_disks", return_value=[selected]), \
                 mock.patch.object(flash, "choose_disk", return_value=selected), \
                 mock.patch.object(flash, "read_disk", return_value=changed), \
                 mock.patch.object(flash, "root_disk_identifier", return_value="disk3"), \
                 mock.patch.object(flash, "confirm_erase"), \
                 mock.patch.object(flash, "flash_image") as writer:
                with self.assertRaises(flash.FlashError):
                    flash.main(["--image", str(image)])
                writer.assert_not_called()

    def test_image_change_after_confirmation_is_rejected_before_raw_open(self):
        with tempfile.TemporaryDirectory() as tmp:
            image, _ = self.make_image(tmp)
            target = pathlib.Path(tmp) / "card.raw"
            target.write_bytes(b"\0" * 20000)
            candidate = dataclasses.replace(disk(), raw_path=str(target))
            with mock.patch.object(flash.os, "geteuid", return_value=0), \
                 mock.patch.object(flash, "require_same_disk") as identity:
                with self.assertRaises(flash.FlashError):
                    flash.root_write(str(image), "0" * 64, flash.disk_json(candidate))
                identity.assert_not_called()

    def test_sudo_authentication_precedes_unmount(self):
        candidate = disk()
        calls = []
        def fake_run(argv, **kwargs):
            calls.append(tuple(argv))
            return mock.Mock(stdout=b'', stderr=b'')
        with tempfile.TemporaryDirectory() as tmp:
            image, _ = self.make_image(tmp)
            worker = mock.Mock(stdout=b'{"size":10000,"sha256":"abc","verified_sha256":"abc"}')
            with mock.patch.object(flash, "run", side_effect=fake_run), \
                 mock.patch.object(flash, "require_same_disk", return_value=candidate) as identity, \
                 mock.patch.object(flash.subprocess, "run", return_value=worker):
                flash.flash_image(image, flash.expected_checksum(image), candidate)
        self.assertEqual(calls[0], ("/usr/bin/sudo", "-v"))
        self.assertEqual(calls[1], ("/usr/sbin/diskutil", "unmountDisk", candidate.path))
        self.assertEqual(identity.call_count, 2)

    def test_swap_during_sudo_or_unmount_is_rejected_before_dd(self):
        candidate = disk()
        for identities in ([flash.FlashError("changed")], [candidate, flash.FlashError("changed")]):
            with self.subTest(identities=identities), tempfile.TemporaryDirectory() as tmp:
                image, _ = self.make_image(tmp)
                with mock.patch.object(flash, "run", return_value=mock.Mock(stdout=b'', stderr=b'')), \
                     mock.patch.object(flash, "require_same_disk", side_effect=identities), \
                     mock.patch.object(flash.subprocess, "run") as process:
                    with self.assertRaises(flash.FlashError):
                        flash.flash_image(image, flash.expected_checksum(image), candidate)
                    process.assert_not_called()

    def test_write_failure_ejects_partial_card(self):
        with tempfile.TemporaryDirectory() as tmp:
            image, payload = self.make_image(tmp)
            candidate = disk(size=len(payload) + 1024)
            commands = []
            def fake_run(argv, **kwargs):
                commands.append(tuple(argv)); return mock.Mock(stdout=b'', stderr=b'')
            with mock.patch.object(flash.sys, "platform", "darwin"), \
                 mock.patch.object(flash, "list_external_disks", return_value=[candidate]), \
                 mock.patch.object(flash, "choose_disk", return_value=candidate), \
                 mock.patch.object(flash, "read_disk", return_value=candidate), \
                 mock.patch.object(flash, "root_disk_identifier", return_value="disk3"), \
                 mock.patch.object(flash, "confirm_erase"), \
                 mock.patch.object(flash, "flash_image", side_effect=flash.FlashError("write failed")), \
                 mock.patch.object(flash, "eject_same_disk") as eject, \
                 mock.patch.object(flash, "run", side_effect=fake_run):
                with self.assertRaises(flash.FlashError):
                    flash.main(["--image", str(image)])
            eject.assert_called_once_with(candidate, strict=False)

    def test_postwrite_identity_allows_media_uuid_change_only(self):
        before = disk(media_identity=("before-write",))
        after = disk(media_identity=("after-write",))
        with mock.patch.object(flash, "external_physical_identifiers", return_value={"disk9"}), \
             mock.patch.object(flash, "read_disk", return_value=after):
            self.assertEqual(flash.require_same_physical_disk(before), after)
        self.assertNotEqual(before.prewrite_fingerprint, after.prewrite_fingerprint)
        self.assertEqual(before.postwrite_fingerprint, after.postwrite_fingerprint)

    def test_external_physical_membership_is_required(self):
        candidate = disk()
        with mock.patch.object(flash, "external_physical_identifiers", return_value=set()), \
             mock.patch.object(flash, "read_disk") as reader:
            with self.assertRaises(flash.FlashError):
                flash.require_same_disk(candidate)
            reader.assert_not_called()

    def test_readback_revalidates_before_opening_raw_device(self):
        candidate = disk()
        with mock.patch.object(flash, "run", return_value=mock.Mock(stdout=b'', stderr=b'')), \
             mock.patch.object(flash, "require_same_physical_disk", side_effect=flash.FlashError("changed")), \
             mock.patch.object(flash.subprocess, "run") as process:
            with self.assertRaises(flash.FlashError):
                flash.verify_written(candidate, 1024, "0" * 64)
            process.assert_not_called()

    def test_cleanup_refuses_to_eject_reused_identifier(self):
        candidate = disk()
        with mock.patch.object(flash, "require_same_physical_disk", side_effect=flash.FlashError("changed")), \
             mock.patch.object(flash, "run") as command:
            self.assertFalse(flash.eject_same_disk(candidate, strict=False))
            command.assert_not_called()

    def test_root_workers_write_and_read_regular_file_by_pinned_descriptor(self):
        with tempfile.TemporaryDirectory() as tmp:
            image, payload = self.make_image(tmp, b"worker-payload" * 1000)
            target = pathlib.Path(tmp) / "card.raw"
            target.write_bytes(b"\0" * (len(payload) + 4096))
            candidate = dataclasses.replace(disk(), raw_path=str(target), size=len(payload) + 4096)
            output = io.StringIO()
            with mock.patch.object(flash.os, "geteuid", return_value=0), \
                 mock.patch.object(flash, "disk_from_json", return_value=candidate), \
                 mock.patch.object(flash, "require_same_disk", return_value=candidate), \
                 mock.patch.object(flash, "root_disk_identifier", return_value="disk3"), \
                 contextlib.redirect_stdout(output):
                flash.root_write(str(image), flash.expected_checksum(image), flash.disk_json(candidate))
            result = json.loads(output.getvalue())
            self.assertEqual(result["size"], len(payload))
            self.assertEqual(result["verified_sha256"], hashlib.sha256(payload).hexdigest())
            self.assertEqual(target.read_bytes()[:len(payload)], payload)
            output = io.StringIO()
            with mock.patch.object(flash.os, "geteuid", return_value=0), \
                 mock.patch.object(flash, "disk_from_json", return_value=candidate), \
                 mock.patch.object(flash, "require_same_physical_disk", return_value=candidate), \
                 contextlib.redirect_stdout(output):
                flash.root_read(str(len(payload)), result["sha256"], flash.disk_json(candidate))
            self.assertEqual(output.getvalue().strip(), result["sha256"])

    def test_root_write_rejects_external_system_disk_after_fd_open(self):
        with tempfile.TemporaryDirectory() as tmp:
            image, _ = self.make_image(tmp)
            target = pathlib.Path(tmp) / "card.raw"
            target.write_bytes(b"\0" * 20000)
            candidate = dataclasses.replace(disk(), raw_path=str(target))
            with mock.patch.object(flash.os, "geteuid", return_value=0), \
                 mock.patch.object(flash, "disk_from_json", return_value=candidate), \
                 mock.patch.object(flash, "require_same_disk", return_value=candidate), \
                 mock.patch.object(flash, "root_disk_identifier", return_value="disk9"):
                with self.assertRaises(flash.FlashError):
                    flash.root_write(str(image), flash.expected_checksum(image), flash.disk_json(candidate))
            self.assertEqual(target.read_bytes(), b"\0" * 20000)

    def test_worker_rejects_raw_path_not_derived_from_identifier(self):
        payload = json.loads(flash.disk_json(disk()))
        payload["raw_path"] = "/dev/rdisk3"
        with self.assertRaises(flash.FlashError):
            flash.disk_from_json(json.dumps(payload))

    def test_stream_and_regular_file_readback_hashes_match(self):
        with tempfile.TemporaryDirectory() as tmp:
            image, payload = self.make_image(tmp, b"0123456789" * 10000)
            destination = pathlib.Path(tmp) / "card.raw"
            with destination.open("wb") as sink:
                size, digest = flash.uncompressed_size_and_hash(image, sink)
            self.assertEqual(size, len(payload))
            self.assertEqual(destination.read_bytes(), payload)
            self.assertEqual(digest, hashlib.sha256(payload).hexdigest())


if __name__ == "__main__":
    unittest.main()
