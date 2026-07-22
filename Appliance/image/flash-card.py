#!/usr/bin/env python3
"""Safely flash an F11 release image to an external SD card on macOS."""
import argparse
import dataclasses

import hashlib
import lzma
import os
import pathlib
import plistlib
import json
import re
import subprocess
import sys
import time

CHUNK = 4 * 1024 * 1024


class FlashError(RuntimeError):
    pass


@dataclasses.dataclass(frozen=True)
class Disk:
    identifier: str
    path: str
    raw_path: str
    model: str
    size: int
    protocol: str
    internal: bool
    whole: bool
    writable: bool
    virtual: bool
    removable: bool
    ejectable: bool
    mounts: tuple[str, ...]
    media_identity: tuple[str, ...]
    physical_identity: tuple[str, ...]

    @property
    def prewrite_fingerprint(self):
        return (self.identifier, self.size, self.model, self.protocol, self.internal,
                self.whole, self.writable, self.virtual, self.removable, self.ejectable,
                self.media_identity, self.physical_identity)

    @property
    def postwrite_fingerprint(self):
        return (self.identifier, self.size, self.model, self.protocol, self.internal,
                self.whole, self.writable, self.virtual, self.removable, self.ejectable,
                self.physical_identity)


def run(argv, *, check=True, capture=True):
    return subprocess.run(argv, check=check, stdout=subprocess.PIPE if capture else None,
                          stderr=subprocess.PIPE if capture else None)


def diskutil_plist(*args):
    try:
        if not args:
            raise FlashError("missing diskutil subcommand")
        result = run(["/usr/sbin/diskutil", args[0], "-plist", *args[1:]])
        return plistlib.loads(result.stdout)
    except (subprocess.CalledProcessError, plistlib.InvalidFileException) as exc:
        raise FlashError(f"diskutil failed for {' '.join(args)}") from exc


def collect_mounts(node):
    found = []
    if isinstance(node, dict):
        mount = node.get("MountPoint")
        if isinstance(mount, str) and mount:
            found.append(mount)
        for value in node.values():
            found.extend(collect_mounts(value))
    elif isinstance(node, list):
        for value in node:
            found.extend(collect_mounts(value))
    return found


def card_reader_media():
    try:
        result = run(["/usr/sbin/system_profiler", "SPCardReaderDataType", "-json"])
        payload = json.loads(result.stdout)
    except (subprocess.CalledProcessError, json.JSONDecodeError) as exc:
        raise FlashError("could not inspect the built-in SD card reader") from exc
    found = {}
    for reader in payload.get("SPCardReaderDataType", []):
        for card in reader.get("_items", []):
            identifier = explicit_whole_identifier(card.get("bsd_name"))
            serial = card.get("spcardreader_card_serialnumber")
            if identifier and serial and card.get("removable_media") == "yes":
                found[identifier] = {
                    "serial": str(serial),
                    "product": str(card.get("spcardreader_card_productname") or "SD card"),
                    "size": int(card.get("size_in_bytes") or 0),
                }
    return found


def whole_identifier(value):
    if not isinstance(value, str):
        return ""
    match = re.fullmatch(r"(?:/dev/)?(disk\d+)(?:s\d+)?", value)
    return match.group(1) if match else ""


def explicit_whole_identifier(value):
    if not isinstance(value, str):
        return ""
    match = re.fullmatch(r"(?:/dev/)?(disk\d+)", value)
    return match.group(1) if match else ""


def root_disk_identifier():
    info = diskutil_plist("info", "/")
    for key in ("ParentWholeDisk", "PartOfWhole", "DeviceIdentifier"):
        identifier = whole_identifier(info.get(key))
        if identifier:
            return identifier
    raise FlashError("could not identify the current macOS system disk")


def read_disk(identifier, listing=None, listed_whole=False):
    if not re.fullmatch(r"disk\d+", identifier):
        raise FlashError("destination must be a whole disk such as /dev/disk4")
    info = diskutil_plist("info", f"/dev/{identifier}")
    if info.get("DeviceIdentifier") != identifier:
        raise FlashError("diskutil returned a different device than requested")
    mounts = tuple(sorted(set(collect_mounts(listing or {}))))
    built_in_card = card_reader_media().get(identifier)
    media_identity = (str(info["MediaUUID"]),) if info.get("MediaUUID") else ()
    if built_in_card:
        media_identity = ("sd-serial:" + built_in_card["serial"],)
    identity = tuple(str(info[key]) for key in (
        "DeviceTreePath", "DeviceLocation"
    ) if info.get(key))
    return Disk(
        identifier=identifier,
        path=f"/dev/{identifier}",
        raw_path=f"/dev/r{identifier}",
        model=str(info.get("MediaName") or info.get("IORegistryEntryName") or "Unknown media"),
        size=int(info.get("TotalSize") or 0),
        protocol=str(info.get("BusProtocol") or info.get("Protocol") or "Unknown"),
        internal=bool(info.get("Internal", True)),
        whole=bool(info.get("Whole", False) or listed_whole),
        writable=bool(info.get("WritableMedia", info.get("Writable", False))),
        virtual=bool(info.get("VirtualOrPhysical") == "Virtual" or info.get("Virtual", False)),
        removable=bool(info.get("RemovableMedia", info.get("Removable", False))),
        ejectable=bool(info.get("Ejectable", False)),
        mounts=mounts,
        media_identity=media_identity,
        physical_identity=identity,
    )


def list_external_disks():
    listing = diskutil_plist("list", "external", "physical")
    root = root_disk_identifier()
    disks = []
    for node in listing.get("AllDisksAndPartitions", []):
        identifier = whole_identifier(node.get("DeviceIdentifier"))
        if not identifier:
            continue
        disk = read_disk(identifier, node, listed_whole=True)
        if disk.identifier == root or disk.internal or not disk.whole or disk.virtual:
            continue
        disks.append(disk)
    for identifier in sorted(card_reader_media()):
        if any(d.identifier == identifier for d in disks):
            continue
        node = diskutil_plist("list", f"/dev/{identifier}")
        disk = read_disk(identifier, node, listed_whole=True)
        if disk.identifier != root and disk.whole and not disk.virtual:
            disks.append(disk)
    return sorted(disks, key=lambda d: d.identifier)


def external_physical_identifiers():
    listing = diskutil_plist("list", "external", "physical")
    identifiers = {
        whole_identifier(node.get("DeviceIdentifier"))
        for node in listing.get("AllDisksAndPartitions", [])
        if whole_identifier(node.get("DeviceIdentifier"))
    }
    identifiers.update(card_reader_media())
    return identifiers


def require_external_physical(identifier):
    if identifier not in external_physical_identifiers():
        raise FlashError("destination is not present in a fresh external-physical disk listing")


def human_size(size):
    value = float(size)
    for unit in ("B", "KiB", "MiB", "GiB", "TiB"):
        if value < 1024 or unit == "TiB":
            return f"{value:.1f} {unit}"
        value /= 1024
    return f"{size} B"


def expected_checksum(image):
    sidecar = image.with_name(image.name + ".sha256")
    if not sidecar.is_file():
        raise FlashError(f"missing checksum file: {sidecar}")
    fields = sidecar.read_text().strip().split()
    if len(fields) != 2 or not re.fullmatch(r"[0-9a-f]{64}", fields[0]):
        raise FlashError(f"invalid checksum file: {sidecar}")
    if pathlib.Path(fields[1]).name != image.name:
        raise FlashError("checksum file names a different image")
    return fields[0]


def sha256_stream(source):
    digest = hashlib.sha256()
    for chunk in iter(lambda: source.read(CHUNK), b""):
        digest.update(chunk)
    return digest.hexdigest()


def sha256_file(path):
    with path.open("rb") as source:
        return sha256_stream(source)


def verify_release(image):
    expected = expected_checksum(image)
    print(f"Verifying release checksum for {image.name} …", flush=True)
    actual = sha256_file(image)
    if actual != expected:
        raise FlashError(f"image checksum mismatch: expected {expected}, got {actual}")
    print(f"Checksum verified: {actual}")
    return expected


def validate_destination(disk, image_size, root):
    if disk.identifier == root:
        raise FlashError("refusing to overwrite the current macOS system disk")
    built_in_sd = (disk.internal and disk.removable and disk.ejectable and
                   disk.protocol == "Secure Digital" and
                   disk.media_identity and disk.media_identity[0].startswith("sd-serial:"))
    if disk.internal and not built_in_sd:
        raise FlashError("refusing to overwrite an internal disk")
    if not disk.whole or disk.virtual or not disk.writable:
        raise FlashError("destination is not a writable external physical whole disk")
    if not disk.media_identity:
        raise FlashError("macOS did not expose a media UUID for this card; use Raspberry Pi Imager instead")
    if not disk.physical_identity:
        raise FlashError("macOS did not expose a physical location identity for this card; use Raspberry Pi Imager instead")
    if disk.size < image_size:
        raise FlashError(f"card is too small: {human_size(disk.size)} available, {human_size(image_size)} required")


def require_same_disk(expected):
    require_external_physical(expected.identifier)
    current = read_disk(expected.identifier, listed_whole=True)
    if current.prewrite_fingerprint != expected.prewrite_fingerprint:
        raise FlashError("the selected device changed after confirmation; refusing to write")
    return current


def require_same_physical_disk(expected):
    require_external_physical(expected.identifier)
    current = read_disk(expected.identifier, listed_whole=True)
    if current.postwrite_fingerprint != expected.postwrite_fingerprint:
        raise FlashError("the physical device changed after writing")
    return current


def disk_json(disk):
    value = dataclasses.asdict(disk)
    for key in ("mounts", "media_identity", "physical_identity"):
        value[key] = list(value[key])
    return json.dumps(value, separators=(",", ":"))


def disk_from_json(value):
    data = json.loads(value)
    if not isinstance(data, dict):
        raise FlashError("invalid disk identity payload")
    identifier = explicit_whole_identifier(data.get("identifier"))
    if not identifier:
        raise FlashError("invalid whole-disk identifier in worker payload")
    expected_path = f"/dev/{identifier}"
    expected_raw = f"/dev/r{identifier}"
    if data.get("path") != expected_path or data.get("raw_path") != expected_raw:
        raise FlashError("worker disk paths do not match the validated identifier")
    for key in ("mounts", "media_identity", "physical_identity"):
        if not isinstance(data.get(key), list):
            raise FlashError("invalid disk identity payload")
        data[key] = tuple(data[key])
    try:
        return Disk(**data)
    except (TypeError, ValueError) as exc:
        raise FlashError("invalid disk identity payload") from exc


def root_write(image_name, expected_hash, disk_value):
    if os.geteuid() != 0:
        raise FlashError("write worker must run as root")
    if not re.fullmatch(r"[0-9a-f]{64}", expected_hash):
        raise FlashError("invalid image checksum")
    expected = disk_from_json(disk_value)
    image = pathlib.Path(image_name)
    if not image.is_file() or not image.name.endswith(".img.xz"):
        raise FlashError("worker image must be an existing .img.xz file")
    with image.open("rb") as compressed:
        if sha256_stream(compressed) != expected_hash:
            raise FlashError("image changed after confirmation; nothing was erased")
        compressed.seek(0)
        fd = os.open(expected.raw_path, os.O_RDWR)
        try:
            current = require_same_disk(expected)
            validate_destination(current, 1, root_disk_identifier())
            total = 0
            digest = hashlib.sha256()
            started = time.monotonic()
            last_report = started
            with lzma.LZMAFile(compressed, "rb") as source:
                while True:
                    chunk = source.read(CHUNK)
                    if not chunk:
                        break
                    view = memoryview(chunk)
                    while view:
                        written = os.write(fd, view)
                        if written <= 0:
                            raise FlashError("raw device write made no progress")
                        view = view[written:]
                    digest.update(chunk)
                    total += len(chunk)
                    now = time.monotonic()
                    if now - last_report >= 0.5:
                        elapsed = max(now - started, 0.001)
                        print(f"\rWritten {human_size(total)} at {human_size(total / elapsed)}/s",
                              end="", flush=True, file=sys.stderr)
                        last_report = now
            os.fsync(fd)
            os.lseek(fd, 0, os.SEEK_SET)
            remaining = total
            verified = hashlib.sha256()
            while remaining:
                chunk = os.read(fd, min(CHUNK, remaining))
                if not chunk:
                    raise FlashError("SD card ended before pinned-descriptor verification completed")
                verified.update(chunk)
                remaining -= len(chunk)
            verified_hash = verified.hexdigest()
            if verified_hash != digest.hexdigest():
                raise FlashError(
                    f"write verification failed: expected {digest.hexdigest()}, got {verified_hash}")
        finally:
            os.close(fd)
    print(json.dumps({"size": total, "sha256": digest.hexdigest(),
                      "verified_sha256": verified_hash}, separators=(",", ":")))


def root_read(size_value, expected_hash, disk_value):
    if os.geteuid() != 0:
        raise FlashError("read worker must run as root")
    if not re.fullmatch(r"[0-9a-f]{64}", expected_hash):
        raise FlashError("invalid readback checksum")
    try:
        expected_size = int(size_value)
    except ValueError as exc:
        raise FlashError("invalid readback size") from exc
    if expected_size <= 0:
        raise FlashError("invalid readback size")
    expected = disk_from_json(disk_value)
    fd = os.open(expected.raw_path, os.O_RDONLY)
    try:
        require_same_physical_disk(expected)
        remaining = expected_size
        digest = hashlib.sha256()
        while remaining:
            chunk = os.read(fd, min(CHUNK, remaining))
            if not chunk:
                raise FlashError("SD card ended before the complete image was read back")
            digest.update(chunk)
            remaining -= len(chunk)
    finally:
        os.close(fd)
    actual = digest.hexdigest()
    if actual != expected_hash:
        raise FlashError(f"write verification failed: expected {expected_hash}, got {actual}")
    print(actual)


def choose_disk(disks):
    if not disks:
        raise FlashError("no external physical disks found; insert the SD card and try again")
    print("\nExternal physical disks detected:\n")
    for index, disk in enumerate(disks, 1):
        mounted = ", ".join(disk.mounts) if disk.mounts else "not mounted"
        print(f"  {index}) {disk.path}  {human_size(disk.size)}  {disk.model}")
        print(f"     Protocol: {disk.protocol}; volumes: {mounted}")
        identity = disk.media_identity + disk.physical_identity
        print(f"     Identity: {hashlib.sha256(chr(0).join(identity).encode()).hexdigest()[:16]}")
    print("\nCompare the model and capacity with the label on your SD card.")
    while True:
        answer = input(f"Select the SD card [1-{len(disks)}], or q to quit: ").strip().lower()
        if answer == "q":
            raise KeyboardInterrupt
        if answer.isdigit() and 1 <= int(answer) <= len(disks):
            return disks[int(answer) - 1]
        print("Invalid selection.")


def confirm_erase(disk):
    print("\n" + "!" * 72)
    print("WARNING: THIS WILL COMPLETELY ERASE THE SELECTED DISK")
    print("Every partition and every file on the card will be destroyed.")
    print(f"Destination: {disk.path}")
    print(f"Model:       {disk.model}")
    print(f"Capacity:    {human_size(disk.size)}")
    identity = disk.media_identity + disk.physical_identity
    print(f"Identity:    {hashlib.sha256(chr(0).join(identity).encode()).hexdigest()[:16]}")
    print("!" * 72)
    typed = input(f"\nType the exact device name {disk.path} to continue: ").strip()
    if typed != disk.path:
        raise FlashError("device-name confirmation did not match; nothing was erased")
    phrase = f"ERASE {disk.path}"
    typed = input(f"Type {phrase} to authorize complete erasure: ").strip()
    if typed != phrase:
        raise FlashError("erase confirmation did not match; nothing was erased")


def uncompressed_size_and_hash(image, sink=None, progress="Scanned"):
    total = 0
    digest = hashlib.sha256()
    started = time.monotonic()
    last_report = started
    with lzma.LZMAFile(image, "rb") as source:
        while True:
            chunk = source.read(CHUNK)
            if not chunk:
                break
            if sink is not None:
                sink.write(chunk)
            digest.update(chunk)
            total += len(chunk)
            now = time.monotonic()
            if now - last_report >= 0.5:
                elapsed = max(now - started, 0.001)
                print(f"\r{progress} {human_size(total)} at {human_size(total / elapsed)}/s", end="", flush=True)
                last_report = now
    if total:
        elapsed = max(time.monotonic() - started, 0.001)
        print(f"\r{progress} {human_size(total)} at {human_size(total / elapsed)}/s")
    return total, digest.hexdigest()


def flash_image(image, expected_compressed_hash, disk):
    print("Requesting administrator access. macOS may ask for your password.")
    run(["/usr/bin/sudo", "-v"], capture=False)
    disk = require_same_disk(disk)
    print(f"\nUnmounting all volumes on {disk.path} …")
    run(["/usr/sbin/diskutil", "unmountDisk", disk.path], capture=False)
    disk = require_same_disk(disk)
    result = subprocess.run(
        ["/usr/bin/sudo", "-n", "/usr/bin/python3", str(pathlib.Path(__file__).resolve()),
         "--root-write", str(image), expected_compressed_hash, disk_json(disk)],
        check=True, stdout=subprocess.PIPE)
    payload = json.loads(result.stdout)
    if payload.get("verified_sha256") != payload.get("sha256"):
        raise FlashError("privileged writer did not return a valid pinned-descriptor verification")
    return int(payload["size"]), str(payload["sha256"])


def verify_written(disk, expected_size, expected_hash):
    print("Verifying the bytes written to the SD card …", flush=True)
    run(["/usr/bin/sudo", "-v"], capture=False)
    disk = require_same_physical_disk(disk)
    result = subprocess.run(
        ["/usr/bin/sudo", "-n", "/usr/bin/python3", str(pathlib.Path(__file__).resolve()),
         "--root-read", str(expected_size), expected_hash, disk_json(disk)],
        check=True, stdout=subprocess.PIPE)
    actual = result.stdout.decode().strip()
    print(f"Write verified: {actual}")


def eject_same_disk(disk, *, strict):
    try:
        current = require_same_physical_disk(disk)
    except FlashError:
        if strict:
            raise
        print("WARNING: device identity changed; refusing to eject the reused disk identifier.", file=sys.stderr)
        return False
    run(["/usr/sbin/diskutil", "eject", current.path], check=strict, capture=False)
    return True


def default_image():
    script = pathlib.Path(__file__).resolve()
    candidates = [script.parent]
    if len(script.parents) >= 3:
        candidates.append(script.parents[2] / "dist")
    matches = sorted({p.resolve() for directory in candidates for p in directory.glob("f11-twitch-zero-*.img.xz")})
    if len(matches) != 1:
        raise FlashError("specify --image; expected exactly one f11-twitch-zero image in dist/")
    return matches[0]


def main(argv=None):
    argv = list(sys.argv[1:] if argv is None else argv)
    if argv and argv[0] == "--root-write":
        if len(argv) != 4:
            raise FlashError("invalid root write invocation")
        root_write(argv[1], argv[2], argv[3])
        return 0
    if argv and argv[0] == "--root-read":
        if len(argv) != 4:
            raise FlashError("invalid root read invocation")
        root_read(argv[1], argv[2], argv[3])
        return 0
    parser = argparse.ArgumentParser(description="Safely flash an F11 Raspberry Pi SD card on macOS")
    parser.add_argument("--image", type=pathlib.Path, help="release .img.xz (defaults to the sole image in dist/)")
    parser.add_argument("--device", help="preselect an external whole disk such as /dev/disk4")
    parser.add_argument("--dry-run", action="store_true", help="perform discovery, checksum, and confirmations without writing")
    parser.add_argument("--skip-readback", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args(argv)
    if sys.platform != "darwin":
        raise FlashError("this safety-focused release supports macOS diskutil; no disk was touched")
    image = (args.image or default_image()).expanduser().resolve()
    if not image.is_file() or not image.name.endswith(".img.xz"):
        raise FlashError("image must be an existing .img.xz file")
    release_hash = verify_release(image)
    print("Checking the uncompressed image size …", flush=True)
    image_size, _ = uncompressed_size_and_hash(image)
    disks = list_external_disks()
    if args.device:
        identifier = explicit_whole_identifier(args.device)
        if not identifier:
            raise FlashError("--device must name a whole disk such as /dev/disk4, never a partition")
        require_external_physical(identifier)
        # Syntax establishes a whole-disk target; diskutil info must still return that exact identifier.
        selected = read_disk(identifier, listed_whole=True)
    else:
        selected = choose_disk(disks)
    validate_destination(selected, image_size, root_disk_identifier())
    confirm_erase(selected)
    # Re-read immediately before the destructive action to detect removal or replacement.
    current = read_disk(selected.identifier, listed_whole=True)
    if current.prewrite_fingerprint != selected.prewrite_fingerprint:
        raise FlashError("the selected device changed after confirmation; nothing was erased")
    validate_destination(current, image_size, root_disk_identifier())
    if args.dry_run:
        print("\nDRY RUN COMPLETE: all checks passed; no disk was unmounted or written.")
        return 0
    try:
        written, raw_hash = flash_image(image, release_hash, current)
        if written != image_size:
            raise FlashError("written byte count did not match the validated image size")
        print(f"Write verified through the pinned raw descriptor: {raw_hash}")
    except BaseException:
        eject_same_disk(current, strict=False)
        raise
    eject_same_disk(current, strict=True)
    print("\nFlash complete and SD card ejected safely.")
    print("Remove and reinsert it if you want to run image/personalize-card.py before first boot.")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        print("\nCancelled. If writing had started, the card may be partially erased; reflash it before use.", file=sys.stderr)
        raise SystemExit(130)
    except (FlashError, subprocess.CalledProcessError, lzma.LZMAError) as exc:
        print(f"\nERROR: {exc}", file=sys.stderr)
        raise SystemExit(1)
