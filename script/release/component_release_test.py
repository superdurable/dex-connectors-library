# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

from __future__ import annotations

import importlib.util
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("component_release.py")
SPEC = importlib.util.spec_from_file_location("component_release", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
release = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = release
SPEC.loader.exec_module(release)


class ComponentReleaseTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.repository = Path(self.temporary.name)
        self.git("init", "-b", "main")
        self.git("config", "user.email", "release-test@example.com")
        self.git("config", "user.name", "Release Test")
        module = self.repository / "sdk/go"
        module.mkdir(parents=True)
        (module / "go.mod").write_text("module example.com/connectors/sdk/go\n\ngo 1.24\n", encoding="utf-8")
        (module / "sdk.go").write_text("package connector\n", encoding="utf-8")
        self.commit("sdk(go): initial SDK")

    def git(self, *arguments: str) -> str:
        return subprocess.run(
            ("git", *arguments),
            cwd=self.repository,
            check=True,
            text=True,
            stdout=subprocess.PIPE,
        ).stdout.strip()

    def commit(self, message: str) -> None:
        self.git("add", ".")
        self.git("commit", "-m", message)

    def plan(self, bump: str) -> object:
        previous = Path.cwd()
        os.chdir(self.repository)
        self.addCleanup(os.chdir, previous)
        return release.create_plan("sdk/go", "sdk/go/", bump)

    def test_first_release_is_v010(self) -> None:
        plan = self.plan("minor")
        self.assertEqual(plan.version, "v0.1.0")
        self.assertEqual(plan.tag, "sdk/go/v0.1.0")

    def test_minor_and_patch_follow_latest_component_tag(self) -> None:
        self.git("tag", "sdk/go/v0.1.0")
        (self.repository / "sdk/go/sdk.go").write_text("package connector\n\nconst Version = 2\n", encoding="utf-8")
        self.commit("sdk(go): add API")
        self.assertEqual(self.plan("minor").version, "v0.2.0")
        self.assertEqual(self.plan("patch").version, "v0.1.1")

    def test_major_resets_minor_and_patch(self) -> None:
        self.git("tag", "sdk/go/v0.7.4")
        (self.repository / "sdk/go/sdk.go").write_text("package connector\n\nconst Version = 2\n", encoding="utf-8")
        self.commit("sdk(go): add API")
        self.assertEqual(self.plan("major").version, "v1.0.0")

    def test_unrelated_change_does_not_create_release(self) -> None:
        self.git("tag", "sdk/go/v0.1.0")
        (self.repository / "README.md").write_text("docs\n", encoding="utf-8")
        self.commit("docs: update")
        with self.assertRaisesRegex(ValueError, "no changes"):
            self.plan("minor")

    def test_breaking_notes_reject_v0_patch(self) -> None:
        self.git("tag", "sdk/go/v0.1.0")
        (self.repository / "sdk/go/sdk.go").write_text("package connector\n\nconst Version = 2\n", encoding="utf-8")
        self.commit("sdk(go): replace API (breaking)")
        plan = self.plan("patch")
        previous = Path.cwd()
        os.chdir(self.repository)
        self.addCleanup(os.chdir, previous)
        notes, breaking = release.release_notes(plan, "")
        self.assertTrue(breaking)
        self.assertIn("## Breaking Changes", notes)
        with self.assertRaisesRegex(ValueError, "cannot use a patch"):
            release.validate_breaking_bump(plan, breaking)

    def test_release_ref_must_be_main(self) -> None:
        release.validate_release_ref("refs/heads/main")
        with self.assertRaisesRegex(ValueError, "only from main"):
            release.validate_release_ref("refs/heads/feature")

    def test_v1_breaking_change_requires_major(self) -> None:
        self.git("tag", "sdk/go/v1.2.0")
        (self.repository / "sdk/go/sdk.go").write_text("package connector\n\nconst Version = 2\n", encoding="utf-8")
        self.commit("sdk(go): replace API (breaking)")
        plan = self.plan("minor")
        previous = Path.cwd()
        os.chdir(self.repository)
        self.addCleanup(os.chdir, previous)
        _, breaking = release.release_notes(plan, "")
        with self.assertRaisesRegex(ValueError, "requires a major bump"):
            release.validate_breaking_bump(plan, breaking)


if __name__ == "__main__":
    unittest.main()
