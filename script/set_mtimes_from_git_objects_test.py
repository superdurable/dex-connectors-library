# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Test that derived mtimes follow content, so Go's test cache reruns exactly the changed inputs."""

import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from script.ci_workflow_test import GIT_FIXTURE_ENVIRONMENT  # noqa: E402
from script.set_mtimes_from_git_objects import (  # noqa: E402
    EARLIEST_DERIVED_MTIME,
    derive_mtime_from_object_id,
    set_mtimes_from_git_objects,
)


class SetMtimesFromGitObjectsTest(unittest.TestCase):
    def setUp(self) -> None:
        self.repository = Path(tempfile.mkdtemp())
        self.git("init", "-q", "-b", "main")
        (self.repository / "testdata").mkdir()
        (self.repository / "testdata" / "changed.json").write_text("{}\n")
        (self.repository / "testdata" / "unchanged.json").write_text("[]\n")
        (self.repository / "other").mkdir()
        (self.repository / "other" / "file.txt").write_text("same\n")
        os.symlink("other/file.txt", self.repository / "link")
        self.commit("first")

    def git(self, *arguments: str) -> str:
        return subprocess.run(["git", *arguments], cwd=self.repository, capture_output=True, text=True, check=True,
                              env={**os.environ, **GIT_FIXTURE_ENVIRONMENT}).stdout.strip()

    def commit(self, message: str) -> None:
        self.git("add", "-A")
        self.git("commit", "-q", "-m", message)

    def mtimes(self) -> dict[str, int]:
        paths = (".", "testdata", "testdata/changed.json", "testdata/unchanged.json", "other", "other/file.txt")
        return {path: int((self.repository / path).stat().st_mtime) for path in paths}

    def test_every_tracked_path_gets_its_object_time(self) -> None:
        set_mtimes_from_git_objects(self.repository)
        mtimes = self.mtimes()
        self.assertEqual(derive_mtime_from_object_id(self.git("rev-parse", "HEAD:testdata/changed.json")), mtimes["testdata/changed.json"])
        self.assertEqual(derive_mtime_from_object_id(self.git("rev-parse", "HEAD:testdata")), mtimes["testdata"])
        self.assertEqual(derive_mtime_from_object_id(self.git("rev-parse", "HEAD^{tree}")), mtimes["."])
        for path, mtime in mtimes.items():
            self.assertGreaterEqual(mtime, EARLIEST_DERIVED_MTIME, path)
            self.assertLess(mtime, EARLIEST_DERIVED_MTIME + (1 << 30), path)
        self.assertNotEqual(mtimes["testdata/changed.json"], mtimes["testdata/unchanged.json"])

    def test_a_fresh_checkout_of_the_same_content_gets_the_same_times(self) -> None:
        set_mtimes_from_git_objects(self.repository)
        before = self.mtimes()
        for path in before:
            os.utime(self.repository / path)
        set_mtimes_from_git_objects(self.repository)
        self.assertEqual(before, self.mtimes())

    def test_a_content_change_moves_only_the_changed_file_and_its_directories(self) -> None:
        set_mtimes_from_git_objects(self.repository)
        before = self.mtimes()
        (self.repository / "testdata" / "changed.json").write_text("{\"a\": 1}\n")
        self.commit("second")
        set_mtimes_from_git_objects(self.repository)
        after = self.mtimes()
        changed = {path for path in before if before[path] != after[path]}
        self.assertEqual({".", "testdata", "testdata/changed.json"}, changed)


if __name__ == "__main__":
    unittest.main()
