#!/usr/bin/env python3
"""Apply bounded retention only when the fleet log directory is tmpfs."""

import os
import re
import stat
import subprocess
import sys
import time

LOG_DIR = "/var/log/vif-fleet"
KEEP_ROTATED = 2
ROTATED_GRACE_SECONDS = 5 * 60
STALE_SECONDS = 5 * 60 * 60
SESSION = r"[a-z0-9](?:[a-z0-9-]*[a-z0-9])?"
ACTIVE = re.compile(rf"^{SESSION}\.jsonl$")
ROTATED = re.compile(
    rf"^({SESSION})_[0-9]{{6}}_[0-9]{{6}}(?:_[0-9]+)?\.jsonl$"
)


def main() -> int:
    result = subprocess.run(
        ["/usr/bin/findmnt", "-rn", "-o", "FSTYPE", "--target", LOG_DIR],
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0 or result.stdout.strip() != "tmpfs":
        print(f"refusing cleanup: {LOG_DIR} is not tmpfs", file=sys.stderr)
        return 1

    directory = os.open(LOG_DIR, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    now = time.time_ns()
    stale = []
    rotated = {}
    try:
        with os.scandir(directory) as entries:
            for entry in entries:
                match = ROTATED.fullmatch(entry.name)
                if not match and not ACTIVE.fullmatch(entry.name):
                    continue
                info = entry.stat(follow_symlinks=False)
                if not stat.S_ISREG(info.st_mode):
                    continue
                age = (now - info.st_mtime_ns) / 1_000_000_000
                item = (entry.name, info.st_mtime_ns, info.st_size, age)
                if age >= STALE_SECONDS:
                    stale.append(item)
                elif match:
                    rotated.setdefault(match.group(1), []).append(item)

        remove = {item[0]: item for item in stale}
        for files in rotated.values():
            files.sort(key=lambda item: (item[1], item[0]), reverse=True)
            for item in files[KEEP_ROTATED:]:
                if item[3] >= ROTATED_GRACE_SECONDS:
                    remove[item[0]] = item

        removed_files = 0
        removed_bytes = 0
        failed = False
        for name, _, size, _ in remove.values():
            try:
                os.unlink(name, dir_fd=directory)
                removed_files += 1
                removed_bytes += size
            except FileNotFoundError:
                continue
            except OSError as exc:
                failed = True
                print(f"cannot remove {name}: {exc}", file=sys.stderr)
        print(f"removed_files={removed_files} removed_bytes={removed_bytes}")
        return int(failed)
    finally:
        os.close(directory)


if __name__ == "__main__":
    raise SystemExit(main())
