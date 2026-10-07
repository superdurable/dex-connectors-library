# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

from __future__ import annotations

import importlib.util
import json
import os
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

SCRIPT = Path(__file__).with_name("component_release.py")
SPEC = importlib.util.spec_from_file_location("component_release", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
release = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = release
SPEC.loader.exec_module(release)

LIBRARY = "github.com/superdurable/dex-connectors-library"
SDK_MODULE = f"{LIBRARY}/sdkgo"
COMPLETE_RELEASE_ASSETS = frozenset(
    {"connector-release.json", "connector-release.json.sha256", "connector-release.complete"}
)
SDK_GO_SUM_LINES = (
    f"{SDK_MODULE} v0.1.0 h1:c2RrZ286U0RLCg==\n"
    f"{SDK_MODULE} v0.1.0/go.mod h1:c2RrZ28tbW9kCg==\n"
)
ANTHROPIC_GO_SUM_LINES = (
    f"{LIBRARY}/connectors/anthropic v0.1.0 h1:YW50aHJvcGljCg==\n"
    f"{LIBRARY}/connectors/anthropic v0.1.0/go.mod h1:YW50aHJvcGljLW1vZAo=\n"
)


class OfflineDependencyReleaseLookup(release.DependencyReleaseLookup):
    """Uses the temporary repository's tags and fake GitHub releases and downloads."""

    def __init__(
        self,
        assets_by_tag: dict[str, frozenset[str]] | None = None,
        undownloadable_modules: set[str] | None = None,
        go_sum_lines_by_module: dict[str, str] | None = None,
    ) -> None:
        super().__init__("main")
        self.assets_by_tag = assets_by_tag or {}
        self.undownloadable_modules = undownloadable_modules or set()
        self.go_sum_lines_by_module = go_sum_lines_by_module or {}
        self.release_lookups: list[str] = []
        self.downloads: list[tuple[str, str]] = []

    def release_asset_names(self, tag: str) -> frozenset[str]:
        self.release_lookups.append(tag)
        if tag not in self.assets_by_tag:
            raise ValueError(f"cannot read the GitHub release for {tag}: release not found")
        return self.assets_by_tag[tag]

    def download_module_directly(self, component_path: Path, module: str, version: str) -> None:
        self.downloads.append((module, version))
        if module in self.undownloadable_modules:
            raise ValueError(f"{module}@{version} is not downloadable with GOWORK=off GOPROXY=direct: not found")
        # Like go mod download, add the module's checksums when go.sum lacks them.
        go_sum_lines = self.go_sum_lines_by_module.get(module, "")
        go_sum_path = component_path / "go.sum"
        existing_go_sum = go_sum_path.read_text(encoding="utf-8") if go_sum_path.is_file() else ""
        if go_sum_lines not in existing_go_sum:
            go_sum_path.write_text(existing_go_sum + go_sum_lines, encoding="utf-8")


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

    def run_plan_command(self, *arguments: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            (
                sys.executable,
                str(SCRIPT.resolve()),
                "plan",
                "--component-path", "sdkgo",
                "--tag-prefix", "sdkgo/",
                "--bump", "minor",
                "--ref", "refs/heads/main",
                "--json-output", str(self.repository / "plan.json"),
                "--github-output", str(self.repository / "github-output"),
                *arguments,
            ),
            cwd=self.repository,
            check=False,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )

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

    def enter_repository(self) -> None:
        previous = Path.cwd()
        os.chdir(self.repository)
        self.addCleanup(os.chdir, previous)

    def release_provider_connector(self, directory: str, version: str) -> None:
        module = self.repository / "connectors" / directory
        module.mkdir(parents=True)
        (module / "go.mod").write_text(
            f"module {LIBRARY}/connectors/{directory}\n\ngo 1.24\n\nrequire {SDK_MODULE} v0.1.0\n",
            encoding="utf-8",
        )
        (module / "connector.go").write_text("package provider\n", encoding="utf-8")
        self.commit(f"connector({directory}): release provider")
        self.git("tag", f"connectors/{directory}/{version}")

    def dependent_connector(self, *connector_requirements: str) -> None:
        module = self.repository / "connectors/superdurable/llm"
        module.mkdir(parents=True, exist_ok=True)
        requirements = "".join(f"\t{requirement}\n" for requirement in connector_requirements)
        (module / "go.mod").write_text(
            f"module {LIBRARY}/connectors/superdurable/llm\n\n"
            "go 1.24\n\n"
            "require (\n"
            "\tgithub.com/stretchr/testify v1.11.1\n"
            f"{requirements}"
            f"\t{SDK_MODULE} v0.1.0\n"
            ")\n",
            encoding="utf-8",
        )

    def assert_tag_reaches_head_but_not_main(self, tag: str) -> None:
        self.git("merge-base", "--is-ancestor", tag, "HEAD")
        main_ancestry = subprocess.run(("git", "merge-base", "--is-ancestor", tag, "main"), cwd=self.repository)
        self.assertNotEqual(main_ancestry.returncode, 0, f"{tag} must not be reachable from main")

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

    def test_declared_version_may_skip_versions_but_not_go_back(self) -> None:
        self.git("tag", "sdkgo/v0.7.0")
        (self.repository / "sdkgo/sdk.go").write_text("package sdkgo\n\nconst Version = 2\n", encoding="utf-8")
        self.commit("sdkgo: add API")
        previous = Path.cwd()
        os.chdir(self.repository)
        self.addCleanup(os.chdir, previous)
        self.assertEqual(release.create_plan("sdkgo", "sdkgo/", target_version="v0.7.3").bump, "patch")
        self.assertEqual(release.create_plan("sdkgo", "sdkgo/", target_version="v0.8.0").bump, "minor")
        self.assertEqual(release.create_plan("sdkgo", "sdkgo/", target_version="v0.21.0").bump, "minor")
        self.assertEqual(release.create_plan("sdkgo", "sdkgo/", target_version="v1.0.0").bump, "major")
        with self.assertRaisesRegex(ValueError, "behind latest release"):
            release.create_plan("sdkgo", "sdkgo/", target_version="v0.6.0")

    def test_first_declared_connector_version_joins_the_lockstep_version(self) -> None:
        self.assertEqual(release.version_bump("", "v0.21.0"), "minor")
        with self.assertRaisesRegex(ValueError, "must be after"):
            release.version_bump("v0.21.0", "v0.21.0")

    def test_declared_version_can_recover_an_existing_reachable_tag(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        (self.repository / "sdkgo/sdk.go").write_text("package sdkgo\n\nconst Version = 2\n", encoding="utf-8")
        self.commit("sdkgo: add API")
        tagged_commit = self.git("rev-parse", "HEAD")
        self.git("tag", "sdkgo/v0.2.0")
        (self.repository / "README.md").write_text("later docs\n", encoding="utf-8")
        self.commit("docs: later change")
        previous = Path.cwd()
        os.chdir(self.repository)
        self.addCleanup(os.chdir, previous)
        plan = release.create_plan("sdkgo", "sdkgo/", target_version="v0.2.0")
        self.assertEqual(plan.baseline_tag, "sdkgo/v0.1.0")
        self.assertEqual(plan.source_sha, tagged_commit)
        self.assertEqual(len(plan.commits), 1)

    def test_unrelated_change_does_not_create_release(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        (self.repository / "README.md").write_text("docs\n", encoding="utf-8")
        self.commit("docs: update")
        with self.assertRaisesRegex(ValueError, "no changes"):
            self.plan("minor")

    def test_plan_command_skips_unchanged_sdk_only_when_requested(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        (self.repository / "README.md").write_text("docs\n", encoding="utf-8")
        self.commit("docs: update")
        strict_result = self.run_plan_command()
        self.assertEqual(strict_result.returncode, 1)
        self.assertIn("no changes since sdkgo/v0.1.0", strict_result.stderr)
        self.assertFalse((self.repository / "github-output").exists())

        skip_result = self.run_plan_command("--skip-unchanged")
        self.assertEqual(skip_result.returncode, 0, skip_result.stderr)
        self.assertIn("skipping release", skip_result.stdout)
        self.assertEqual((self.repository / "github-output").read_text(), "release_required=false\n")
        self.assertFalse((self.repository / "plan.json").exists())
        self.assertEqual(self.git("tag", "--list"), "sdkgo/v0.1.0")

    def test_plan_command_preserves_first_and_changed_releases_with_skip_unchanged(self) -> None:
        for baseline in ("", "sdkgo/v0.1.0"):
            with self.subTest(baseline=baseline):
                if baseline:
                    self.git("tag", baseline)
                    (self.repository / "sdkgo/sdk.go").write_text("package sdkgo\n\nconst Version = 2\n", encoding="utf-8")
                    self.commit("sdkgo: add API")
                result = self.run_plan_command("--skip-unchanged")
                self.assertEqual(result.returncode, 0, result.stderr)
                plan = json.loads((self.repository / "plan.json").read_text())
                expected_version = "v0.2.0" if baseline else "v0.1.0"
                self.assertEqual(plan["version"], expected_version)
                self.assertEqual(plan["source_sha"], self.git("rev-parse", "HEAD"))
                self.assertTrue(plan["commits"])
                self.assertIn("release_required=true\n", (self.repository / "github-output").read_text())

    def test_skip_unchanged_does_not_suppress_invalid_release_inputs(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        for arguments, expected_error in (
            (("--bump", "invalid"), "invalid version bump"),
            (("--ref", "refs/heads/feature"), "only from main"),
            (("--component-path", "missing"), "not a Go module"),
        ):
            with self.subTest(arguments=arguments):
                result = self.run_plan_command("--skip-unchanged", *arguments)
                self.assertEqual(result.returncode, 1)
                self.assertIn(expected_error, result.stderr)
                self.assertFalse((self.repository / "github-output").exists())
                self.assertFalse((self.repository / "plan.json").exists())

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
        self.enter_repository()
        lookup = OfflineDependencyReleaseLookup()
        self.assertEqual(
            release.validate_connector("connectors/openai", SDK_MODULE, lookup),
            ("sdkgo/v0.1.0",),
        )
        self.assertEqual(lookup.downloads, [(SDK_MODULE, "v0.1.0")])
        self.assertEqual(lookup.release_lookups, [])

    def test_connector_rejects_replace_and_pseudo_version(self) -> None:
        module = self.connector(
            "\nreplace github.com/superdurable/dex-connectors-library/sdkgo => ../../sdkgo\n"
        )
        self.enter_repository()
        lookup = OfflineDependencyReleaseLookup()
        with self.assertRaisesRegex(ValueError, "replace directives"):
            release.validate_connector("connectors/openai", SDK_MODULE, lookup)
        (module / "go.mod").write_text(
            "module example.com/connectors/openai\n\n"
            "go 1.24\n\n"
            "require github.com/superdurable/dex-connectors-library/sdkgo v0.0.0-20260923000000-deadbeefdead\n",
            encoding="utf-8",
        )
        with self.assertRaisesRegex(ValueError, "invalid stable semantic version"):
            release.validate_connector("connectors/openai", SDK_MODULE, lookup)
        self.assertEqual(lookup.downloads, [])

    def test_connector_requires_exactly_one_sdk_version(self) -> None:
        module = self.connector()
        (module / "go.mod").write_text("module example.com/connectors/openai\n\ngo 1.24\n", encoding="utf-8")
        self.enter_repository()
        with self.assertRaisesRegex(ValueError, "exactly one"):
            release.validate_connector("connectors/openai", SDK_MODULE, OfflineDependencyReleaseLookup())

    def test_connector_sdk_release_tag_must_exist(self) -> None:
        module = self.connector()
        (module / "go.mod").write_text(
            f"module example.com/connectors/openai\n\ngo 1.24\n\nrequire {SDK_MODULE} v0.2.0\n",
            encoding="utf-8",
        )
        self.enter_repository()
        lookup = OfflineDependencyReleaseLookup()
        with self.assertRaisesRegex(ValueError, "connector SDK release tag is missing: sdkgo/v0.2.0"):
            release.validate_connector("connectors/openai", SDK_MODULE, lookup)
        self.assertEqual(lookup.downloads, [])

    def test_connector_sdk_release_must_be_reachable_from_main_not_only_head(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        self.git("switch", "--quiet", "-c", "pull-request")
        (self.repository / "sdkgo/sdk.go").write_text("package sdkgo\n\nconst Version = 2\n", encoding="utf-8")
        self.commit("sdkgo: add API on an unmerged branch")
        self.git("tag", "sdkgo/v0.2.0")
        module = self.repository / "connectors/openai"
        module.mkdir(parents=True)
        (module / "go.mod").write_text(
            f"module example.com/connectors/openai\n\ngo 1.24\n\nrequire {SDK_MODULE} v0.2.0\n",
            encoding="utf-8",
        )
        self.commit("connector(openai): pin the unmerged SDK")
        self.assert_tag_reaches_head_but_not_main("sdkgo/v0.2.0")
        self.enter_repository()
        lookup = OfflineDependencyReleaseLookup()
        with self.assertRaisesRegex(ValueError, "connector SDK release is not reachable from main: sdkgo/v0.2.0"):
            release.validate_connector("connectors/openai", SDK_MODULE, lookup)
        self.assertEqual(lookup.downloads, [])

    def test_connector_accepts_released_complete_connector_dependencies(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        self.release_provider_connector("anthropic", "v0.1.0")
        self.release_provider_connector("google/gemini", "v0.3.0")
        self.dependent_connector(
            f"{LIBRARY}/connectors/anthropic v0.1.0",
            f"{LIBRARY}/connectors/google/gemini v0.3.0 // indirect",
            "google.golang.org/genproto/googleapis/rpc v0.0.0-20260923000000-deadbeefdead // indirect",
        )
        self.commit("connector(llm): add dependent connector")
        self.enter_repository()
        lookup = OfflineDependencyReleaseLookup(
            {
                "connectors/anthropic/v0.1.0": COMPLETE_RELEASE_ASSETS,
                "connectors/google/gemini/v0.3.0": COMPLETE_RELEASE_ASSETS,
            }
        )
        self.assertEqual(
            release.validate_connector("connectors/superdurable/llm", SDK_MODULE, lookup),
            ("sdkgo/v0.1.0", "connectors/anthropic/v0.1.0", "connectors/google/gemini/v0.3.0"),
        )
        self.assertEqual(lookup.release_lookups, ["connectors/anthropic/v0.1.0", "connectors/google/gemini/v0.3.0"])
        self.assertEqual(
            lookup.downloads,
            [
                (SDK_MODULE, "v0.1.0"),
                (f"{LIBRARY}/connectors/anthropic", "v0.1.0"),
                (f"{LIBRARY}/connectors/google/gemini", "v0.3.0"),
            ],
        )

    def test_connector_dependency_rejects_unreleased_versions(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        self.enter_repository()
        for version in ("v0.0.0-20260923000000-deadbeefdead", "v0.7.1-0.20260923000000-deadbeefdead", "v0.7.0-rc.1"):
            with self.subTest(version=version):
                self.dependent_connector(f"{LIBRARY}/connectors/openai {version}")
                lookup = OfflineDependencyReleaseLookup()
                with self.assertRaisesRegex(
                    ValueError, "connectors/openai must pin a released version: invalid stable semantic version"
                ):
                    release.validate_connector("connectors/superdurable/llm", SDK_MODULE, lookup)
                self.assertEqual(lookup.release_lookups, [])
                self.assertEqual(lookup.downloads, [])

    def test_connector_dependency_rejects_branch_and_commit_versions(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        self.enter_repository()
        for version in ("main", "deadbeefdead"):
            with self.subTest(version=version):
                self.dependent_connector(f"{LIBRARY}/connectors/openai {version}")
                lookup = OfflineDependencyReleaseLookup()
                with self.assertRaisesRegex(ValueError, "must be of the form v1.2.3"):
                    release.validate_connector("connectors/superdurable/llm", SDK_MODULE, lookup)
                self.assertEqual(lookup.downloads, [])

    def test_connector_dependency_release_tag_must_exist(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        self.dependent_connector(f"{LIBRARY}/connectors/openai v0.7.0")
        self.commit("connector(llm): add dependent connector")
        self.enter_repository()
        lookup = OfflineDependencyReleaseLookup({"connectors/openai/v0.7.0": COMPLETE_RELEASE_ASSETS})
        with self.assertRaisesRegex(ValueError, "release tag is missing: connectors/openai/v0.7.0"):
            release.validate_connector("connectors/superdurable/llm", SDK_MODULE, lookup)
        self.assertEqual(lookup.release_lookups, [])
        self.assertEqual(lookup.downloads, [])

    def test_connector_dependency_release_must_be_reachable_from_main(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        self.git("switch", "--quiet", "-c", "unmerged-provider")
        self.release_provider_connector("openai", "v0.7.0")
        self.git("switch", "--quiet", "main")
        self.dependent_connector(f"{LIBRARY}/connectors/openai v0.7.0")
        self.commit("connector(llm): add dependent connector")
        self.enter_repository()
        lookup = OfflineDependencyReleaseLookup({"connectors/openai/v0.7.0": COMPLETE_RELEASE_ASSETS})
        with self.assertRaisesRegex(ValueError, "not reachable from main: connectors/openai/v0.7.0"):
            release.validate_connector("connectors/superdurable/llm", SDK_MODULE, lookup)
        self.assertEqual(lookup.downloads, [])

    def test_connector_dependency_release_must_be_reachable_from_main_not_only_head(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        self.git("switch", "--quiet", "-c", "pull-request")
        self.release_provider_connector("openai", "v0.7.0")
        self.dependent_connector(f"{LIBRARY}/connectors/openai v0.7.0")
        self.commit("connector(llm): add dependent connector")
        self.assert_tag_reaches_head_but_not_main("connectors/openai/v0.7.0")
        self.enter_repository()
        lookup = OfflineDependencyReleaseLookup({"connectors/openai/v0.7.0": COMPLETE_RELEASE_ASSETS})
        with self.assertRaisesRegex(
            ValueError, "connector dependency release is not reachable from main: connectors/openai/v0.7.0"
        ):
            release.validate_connector("connectors/superdurable/llm", SDK_MODULE, lookup)
        self.assertEqual(lookup.release_lookups, [])
        self.assertEqual(lookup.downloads, [])

    def test_connector_rejects_other_modules_from_this_repository(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        self.enter_repository()
        for requirement in (
            f"{LIBRARY} v0.1.1-0.20260928041452-7b7fbe591824",
            f"{LIBRARY} v0.1.0",
            f"{LIBRARY}/sdkgo/v2 v2.0.0-20260928041452-7b7fbe591824",
            f"{LIBRARY}/examples/app v0.1.0",
            f"{LIBRARY}/connectors v0.1.0",
        ):
            with self.subTest(requirement=requirement):
                self.dependent_connector(requirement)
                lookup = OfflineDependencyReleaseLookup()
                with self.assertRaisesRegex(
                    ValueError,
                    "may require only the SDK and released connector modules from this repository: "
                    f"{re.escape(requirement.split()[0])}$",
                ):
                    release.validate_connector("connectors/superdurable/llm", SDK_MODULE, lookup)
                self.assertEqual(lookup.downloads, [])

    def test_connector_download_rejects_missing_go_sum_entries_without_editing_go_sum(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        self.release_provider_connector("anthropic", "v0.1.0")
        self.dependent_connector(f"{LIBRARY}/connectors/anthropic v0.1.0")
        self.commit("connector(llm): add dependent connector")
        self.enter_repository()
        go_sum_path = Path("connectors/superdurable/llm/go.sum")
        go_sum_path.write_text(SDK_GO_SUM_LINES, encoding="utf-8")
        stale_go_sum = go_sum_path.read_bytes()
        lookup = self.go_sum_writing_lookup()
        with self.assertRaisesRegex(
            ValueError,
            re.escape(
                f"connectors/superdurable/llm/go.sum is missing entries for {LIBRARY}/connectors/anthropic@v0.1.0; "
                "run go mod tidy"
            ),
        ):
            release.validate_connector("connectors/superdurable/llm", SDK_MODULE, lookup)
        self.assertEqual(go_sum_path.read_bytes(), stale_go_sum)
        self.assertEqual(lookup.downloads, [(SDK_MODULE, "v0.1.0"), (f"{LIBRARY}/connectors/anthropic", "v0.1.0")])

        go_sum_path.unlink()
        with self.assertRaisesRegex(ValueError, re.escape(f"go.sum is missing entries for {SDK_MODULE}@v0.1.0")):
            release.validate_connector("connectors/superdurable/llm", SDK_MODULE, self.go_sum_writing_lookup())
        self.assertFalse(go_sum_path.exists())

    def test_connector_download_accepts_complete_go_sum(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        self.release_provider_connector("anthropic", "v0.1.0")
        self.dependent_connector(f"{LIBRARY}/connectors/anthropic v0.1.0")
        (self.repository / "connectors/superdurable/llm/go.sum").write_text(
            SDK_GO_SUM_LINES + ANTHROPIC_GO_SUM_LINES, encoding="utf-8"
        )
        self.commit("connector(llm): add dependent connector")
        self.enter_repository()
        self.assertEqual(
            release.validate_connector("connectors/superdurable/llm", SDK_MODULE, self.go_sum_writing_lookup()),
            ("sdkgo/v0.1.0", "connectors/anthropic/v0.1.0"),
        )
        self.assertEqual(self.git("status", "--porcelain"), "")

    def go_sum_writing_lookup(self) -> OfflineDependencyReleaseLookup:
        return OfflineDependencyReleaseLookup(
            {"connectors/anthropic/v0.1.0": COMPLETE_RELEASE_ASSETS},
            go_sum_lines_by_module={
                SDK_MODULE: SDK_GO_SUM_LINES,
                f"{LIBRARY}/connectors/anthropic": ANTHROPIC_GO_SUM_LINES,
            },
        )

    def test_connector_dependency_release_must_be_complete(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        self.release_provider_connector("openai", "v0.7.0")
        self.dependent_connector(f"{LIBRARY}/connectors/openai v0.7.0")
        self.commit("connector(llm): add dependent connector")
        self.enter_repository()
        incomplete = OfflineDependencyReleaseLookup(
            {"connectors/openai/v0.7.0": frozenset({"connector-release.json", "connector-release.json.sha256"})}
        )
        with self.assertRaisesRegex(
            ValueError, "incomplete: connectors/openai/v0.7.0 has no connector-release.complete asset"
        ):
            release.validate_connector("connectors/superdurable/llm", SDK_MODULE, incomplete)
        self.assertEqual(incomplete.downloads, [])
        unpublished = OfflineDependencyReleaseLookup()
        with self.assertRaisesRegex(ValueError, "cannot read the GitHub release for connectors/openai/v0.7.0"):
            release.validate_connector("connectors/superdurable/llm", SDK_MODULE, unpublished)
        self.assertEqual(unpublished.downloads, [])

    def test_connector_dependency_must_download_directly(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        self.release_provider_connector("openai", "v0.7.0")
        self.dependent_connector(f"{LIBRARY}/connectors/openai v0.7.0")
        self.commit("connector(llm): add dependent connector")
        self.enter_repository()
        lookup = OfflineDependencyReleaseLookup(
            {"connectors/openai/v0.7.0": COMPLETE_RELEASE_ASSETS},
            undownloadable_modules={f"{LIBRARY}/connectors/openai"},
        )
        with self.assertRaisesRegex(ValueError, "connectors/openai@v0.7.0 is not downloadable"):
            release.validate_connector("connectors/superdurable/llm", SDK_MODULE, lookup)

    def test_connector_dependency_tag_uses_the_module_directory(self) -> None:
        self.assertEqual(
            release.connector_dependency_tag(f"{LIBRARY}/connectors/google/gemini", "v0.3.0"),
            "connectors/google/gemini/v0.3.0",
        )
        self.assertEqual(
            release.connector_dependency_tag(f"{LIBRARY}/connectors/openai/v2", "v2.1.0"),
            "connectors/openai/v2.1.0",
        )

    def test_dependency_lookup_requires_the_main_ref(self) -> None:
        self.git("tag", "sdkgo/v0.1.0")
        self.enter_repository()
        lookup = release.DependencyReleaseLookup("origin/main")
        with self.assertRaisesRegex(ValueError, "main branch ref is missing: origin/main"):
            lookup.is_tag_reachable_from_main("sdkgo/v0.1.0")
        self.assertTrue(release.DependencyReleaseLookup("main").is_tag_reachable_from_main("sdkgo/v0.1.0"))

    def test_dependency_lookup_reads_github_release_assets(self) -> None:
        fake_gh = self.repository / "fake-gh"
        fake_gh.write_text(
            "#!/bin/sh\n"
            'if [ "$1 $2 $4 $5 $6 $7" != "release view --json assets --jq .assets[].name" ]; then exit 2; fi\n'
            'if [ "$3" = connectors/openai/v0.7.0 ]; then\n'
            "  printf 'connector-release.json\\nconnector-release.complete\\n'\n"
            "  exit 0\n"
            "fi\n"
            "echo 'release not found' >&2\n"
            "exit 1\n",
            encoding="utf-8",
        )
        fake_gh.chmod(0o755)
        lookup = release.DependencyReleaseLookup("main")
        with mock.patch.dict(os.environ, {"GH_BIN": str(fake_gh)}):
            self.assertEqual(
                lookup.release_asset_names("connectors/openai/v0.7.0"),
                frozenset({"connector-release.json", "connector-release.complete"}),
            )
            with self.assertRaisesRegex(ValueError, "connectors/openai/v0.8.0: release not found"):
                lookup.release_asset_names("connectors/openai/v0.8.0")

    def test_dependency_lookup_downloads_in_standalone_direct_mode(self) -> None:
        invocations: list[tuple[object, dict[str, object]]] = []
        results = [
            subprocess.CompletedProcess((), 0, "", ""),
            subprocess.CompletedProcess((), 1, "", "unknown revision connectors/openai/v0.7.0\n"),
        ]

        def run_go(command: object, **keywords: object) -> subprocess.CompletedProcess[str]:
            invocations.append((command, keywords))
            return results.pop(0)

        lookup = release.DependencyReleaseLookup("main")
        module = f"{LIBRARY}/connectors/openai"
        with mock.patch.object(release.subprocess, "run", run_go):
            lookup.download_module_directly(self.repository, module, "v0.7.0")
            with self.assertRaisesRegex(ValueError, "not downloadable .*: unknown revision connectors/openai/v0.7.0$"):
                lookup.download_module_directly(self.repository, module, "v0.7.0")
        command, keywords = invocations[0]
        self.assertEqual(command, ("go", "mod", "download", f"{module}@v0.7.0"))
        self.assertEqual(keywords["cwd"], self.repository)
        environment = keywords["env"]
        assert isinstance(environment, dict)
        self.assertEqual(environment["GOWORK"], "off")
        self.assertEqual(environment["GOPROXY"], "direct")
        self.assertEqual(environment["GONOSUMDB"], LIBRARY)

    def test_connector_plan_ignores_other_component_tags(self) -> None:
        self.connector()
        self.git("tag", "connectors/linkedin/v9.9.9")
        previous = Path.cwd()
        os.chdir(self.repository)
        self.addCleanup(os.chdir, previous)
        plan = release.create_plan("connectors/openai", "connectors/openai/", "minor")
        self.assertEqual(plan.version, "v0.1.0")
        self.assertEqual(plan.tag, "connectors/openai/v0.1.0")

    def test_one_commit_can_enter_each_changed_connector_release(self) -> None:
        openai = self.repository / "connectors/openai"
        linkedin = self.repository / "connectors/linkedin"
        openai.mkdir(parents=True)
        linkedin.mkdir(parents=True)
        (openai / "go.mod").write_text("module example.com/openai\n\ngo 1.24\n", encoding="utf-8")
        (linkedin / "go.mod").write_text("module example.com/linkedin\n\ngo 1.24\n", encoding="utf-8")
        (openai / "connector.go").write_text("package openai\n", encoding="utf-8")
        (linkedin / "connector.go").write_text("package linkedin\n", encoding="utf-8")
        self.commit("tooling: update both connector modules")
        previous = Path.cwd()
        os.chdir(self.repository)
        self.addCleanup(os.chdir, previous)
        openai_plan = release.create_plan("connectors/openai", "connectors/openai/", "minor")
        linkedin_plan = release.create_plan("connectors/linkedin", "connectors/linkedin/", "minor")
        self.assertEqual(openai_plan.commits, linkedin_plan.commits)
        self.assertEqual(len(openai_plan.commits), 1)


if __name__ == "__main__":
    unittest.main()
