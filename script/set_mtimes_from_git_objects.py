# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Set every tracked file's and directory's mtime from its Git object ID, for Go's test cache.

Go's test cache keys each file or directory a test opens by its size and mtime. A fresh CI
checkout stamps everything with the checkout time, so test results restored from a main run
never match. A time derived from the object ID changes exactly when the content changes, so
restored results apply to unchanged inputs and rerun for changed ones. Run it only in CI: it
rewrites the working tree's times.
"""

import argparse
import os
import subprocess
import sys
from pathlib import Path

# 1980-01-01 UTC; every derived time is years older than Go's two-second recency cutoff.
EARLIEST_DERIVED_MTIME = 315532800
DERIVED_MTIME_SPAN_SECONDS = 1 << 30


def derive_mtime_from_object_id(object_id: str) -> int:
    """Returns the Unix time that stands for one Git object ID."""
    return EARLIEST_DERIVED_MTIME + int(object_id[:12], 16) % DERIVED_MTIME_SPAN_SECONDS


def list_tracked_objects(repository: Path) -> list[tuple[str, str]]:
    """Returns (relative path, object ID) for HEAD's root tree and every blob and tree below it."""
    root_tree = subprocess.run(["git", "rev-parse", "HEAD^{tree}"], cwd=repository, capture_output=True, text=True,
                               check=True).stdout.strip()
    listing = subprocess.run(["git", "ls-tree", "-r", "-t", "-z", "--full-tree", "HEAD"], cwd=repository,
                             capture_output=True, text=True, check=True).stdout
    objects = [(".", root_tree)]
    for entry in filter(None, listing.split("\0")):
        metadata, path = entry.split("\t", 1)
        mode, object_type, object_id = metadata.split(" ")
        # Symbolic links and submodules are not opened through their own times.
        if object_type in ("blob", "tree") and mode != "120000":
            objects.append((path, object_id))
    return objects


def set_mtimes_from_git_objects(repository: Path) -> int:
    """Sets each tracked path's access and modification times; returns how many paths changed."""
    count = 0
    for path, object_id in list_tracked_objects(repository):
        target = repository / path
        if not target.exists():
            continue
        derived = derive_mtime_from_object_id(object_id)
        os.utime(target, (derived, derived))
        count += 1
    return count


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--repository", type=Path, default=Path.cwd(), help="Git working tree (default: current directory)")
    arguments = parser.parse_args()
    count = set_mtimes_from_git_objects(arguments.repository)
    print(f"Derived mtimes for {count} tracked paths from their Git object IDs.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
