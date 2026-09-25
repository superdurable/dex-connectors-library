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
        module = self.repository / "sdkgo"
        module.mkdir(parents=True)
        (module / "go.mod").write_text("module example.com/connectors/sdkgo\n\ngo 1.24\n", encoding="utf-8")
        (module / "sdk.go").write_text("package sdkgo\n", encoding="utf-8")
        self.commit("sdkgo: initial SDK")

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
        return release.create_plan("sdkgo", "sdkgo/", bump)

    def connector(self, go_mod_suffix: str = "") -> Path:
        self.git("tag", "sdkgo/v0.1.0")
        module = self.repository / "connectors/openai"
        module.mkdir(parents=True)
        (module / "go.mod").write_text(
            "module example.com/connectors/openai\n\n"
            "go 1.24\n\n"
            "require github.com/superdurable/dex-connectors-library/sdkgo v0.1.0\n"
            + go_mod_suffix,
            encoding="utf-8",
        )
        (module / "connector.go").write_text("package openai\n", encoding="utf-8")
        self.commit("connector(openai): add connector")
        return module

    def test_first_release_is_v010(self) -> None:
        plan = self.plan("minor")
        self.assertEqual(plan.version, "v0.1.0")
        self.assertEqual(plan.tag, "sdkgo/v0.1.0")

    def test_minor_and_patch_follow_latest_component_tag(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        (self.repository / "sdkgo/sdk.go").write_text("package sdkgo\n\nconst Version = 2\n", encoding="utf-8")
        self.commit("sdkgo: add API")
        self.assertEqual(self.plan("minor").version, "v0.2.0")
        self.assertEqual(self.plan("patch").version, "v0.1.1")

    def test_major_resets_minor_and_patch(self) -> None:
        self.git("tag", "sdkgo/v0.7.4")
        (self.repository / "sdkgo/sdk.go").write_text("package sdkgo\n\nconst Version = 2\n", encoding="utf-8")
        self.commit("sdkgo: add API")
        self.assertEqual(self.plan("major").version, "v1.0.0")

    def test_unrelated_change_does_not_create_release(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        (self.repository / "README.md").write_text("docs\n", encoding="utf-8")
        self.commit("docs: update")
        with self.assertRaisesRegex(ValueError, "no changes"):
            self.plan("minor")

    def test_breaking_notes_reject_v0_patch(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        (self.repository / "sdkgo/sdk.go").write_text("package sdkgo\n\nconst Version = 2\n", encoding="utf-8")
        self.commit("sdkgo: replace API (breaking)")
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
        self.git("tag", "sdkgo/v1.2.0")
        (self.repository / "sdkgo/sdk.go").write_text("package sdkgo\n\nconst Version = 2\n", encoding="utf-8")
        self.commit("sdkgo: replace API (breaking)")
        plan = self.plan("minor")
        previous = Path.cwd()
        os.chdir(self.repository)
        self.addCleanup(os.chdir, previous)
        _, breaking = release.release_notes(plan, "")
        with self.assertRaisesRegex(ValueError, "requires a major bump"):
            release.validate_breaking_bump(plan, breaking)

    def test_connector_requires_released_sdk_without_replace(self) -> None:
        self.connector()
        previous = Path.cwd()
        os.chdir(self.repository)
        self.addCleanup(os.chdir, previous)
        original_run = subprocess.run

        def run_without_network(*arguments: object, **keywords: object) -> subprocess.CompletedProcess[str]:
            command = arguments[0]
            if isinstance(command, tuple) and command[:3] == ("go", "mod", "download"):
                return subprocess.CompletedProcess(command, 0, "", "")
            return original_run(*arguments, **keywords)

        release.subprocess.run = run_without_network
        self.addCleanup(setattr, release.subprocess, "run", original_run)
        self.assertEqual(
            release.validate_connector(
                "connectors/openai", "github.com/superdurable/dex-connectors-library/sdkgo"
            ),
            "v0.1.0",
        )

    def test_connector_rejects_replace_and_pseudo_version(self) -> None:
        module = self.connector(
            "\nreplace github.com/superdurable/dex-connectors-library/sdkgo => ../../sdkgo\n"
        )
        previous = Path.cwd()
        os.chdir(self.repository)
        self.addCleanup(os.chdir, previous)
        with self.assertRaisesRegex(ValueError, "replace directives"):
            release.validate_connector(
                "connectors/openai", "github.com/superdurable/dex-connectors-library/sdkgo"
            )
        (module / "go.mod").write_text(
            "module example.com/connectors/openai\n\n"
            "go 1.24\n\n"
            "require github.com/superdurable/dex-connectors-library/sdkgo v0.0.0-20260923000000-deadbeefdead\n",
            encoding="utf-8",
        )
        with self.assertRaisesRegex(ValueError, "invalid stable semantic version"):
            release.validate_connector(
                "connectors/openai", "github.com/superdurable/dex-connectors-library/sdkgo"
            )

    def test_connector_plan_ignores_other_component_tags(self) -> None:
        self.connector()
        self.git("tag", "connectors/http/v9.9.9")
        previous = Path.cwd()
        os.chdir(self.repository)
        self.addCleanup(os.chdir, previous)
        plan = release.create_plan("connectors/openai", "connectors/openai/", "minor")
        self.assertEqual(plan.version, "v0.1.0")
        self.assertEqual(plan.tag, "connectors/openai/v0.1.0")

    def test_one_commit_can_enter_each_changed_connector_release(self) -> None:
        openai = self.repository / "connectors/openai"
        http = self.repository / "connectors/http"
        openai.mkdir(parents=True)
        http.mkdir(parents=True)
        (openai / "go.mod").write_text("module example.com/openai\n\ngo 1.24\n", encoding="utf-8")
        (http / "go.mod").write_text("module example.com/http\n\ngo 1.24\n", encoding="utf-8")
        (openai / "connector.go").write_text("package openai\n", encoding="utf-8")
        (http / "connector.go").write_text("package httpconnector\n", encoding="utf-8")
        self.commit("tooling: update both connector modules")
        previous = Path.cwd()
        os.chdir(self.repository)
        self.addCleanup(os.chdir, previous)
        openai_plan = release.create_plan("connectors/openai", "connectors/openai/", "minor")
        http_plan = release.create_plan("connectors/http", "connectors/http/", "minor")
        self.assertEqual(openai_plan.commits, http_plan.commits)
        self.assertEqual(len(openai_plan.commits), 1)


if __name__ == "__main__":
    unittest.main()
