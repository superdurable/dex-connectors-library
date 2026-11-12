# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

from __future__ import annotations

import importlib.util
import io
import hashlib
import http.client
import json
import os
import re
import ssl
import subprocess
import sys
import tempfile
import tarfile
import textwrap
import unittest
import urllib.error
import zipfile
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
NPM_REPOSITORY_URL = "git+https://github.com/superdurable/dex-connectors-library.git"
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

    def release_asset_names(self, tag: str, allow_missing: bool = False) -> frozenset[str]:
        self.release_lookups.append(tag)
        if tag not in self.assets_by_tag:
            if allow_missing:
                return frozenset()
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

    def test_python_and_typescript_release_plans_are_independent(self) -> None:
        python_package = self.repository / "connectors/newsapi/python"
        python_package.mkdir(parents=True)
        (python_package / "pyproject.toml").write_text(
            '[project]\nname = "dex-connector-newsapi"\nversion = "0.2.0"\n', encoding="utf-8"
        )
        type_script_package = self.repository / "connectors/newsapi/typescript"
        type_script_package.mkdir(parents=True)
        (type_script_package / "package.json").write_text(json.dumps({
            "name": "@superdurable/connector-newsapi", "version": "0.1.0",
            "repository": {"type": "git", "url": NPM_REPOSITORY_URL,
                           "directory": "connectors/newsapi/typescript"},
        }), encoding="utf-8")
        self.commit("connector(newsapi): add language packages")
        self.enter_repository()
        python_plan = release.create_plan("connectors/newsapi/python", "connectors/newsapi/python/", target_version="v0.2.0")
        type_script_plan = release.create_plan("connectors/newsapi/typescript", "connectors/newsapi/typescript/", target_version="v0.1.0")
        self.assertEqual(python_plan.module_path, "dex-connector-newsapi")
        self.assertEqual(type_script_plan.module_path, "@superdurable/connector-newsapi")
        self.assertNotEqual(python_plan.tag, type_script_plan.tag)

    def test_plan_does_not_create_a_tag(self) -> None:
        python_package = self.repository / "sdkpython"
        python_package.mkdir()
        (python_package / "pyproject.toml").write_text(
            '[project]\nname = "dex-connectors-sdk"\nversion = "0.1.0"\n', encoding="utf-8"
        )
        self.commit("sdkpython: add SDK")
        json_output = self.repository / "plan.json"
        github_output = self.repository / "github-output"
        result = subprocess.run(
            (sys.executable, str(SCRIPT), "plan", "--component-path", "sdkpython", "--tag-prefix", "sdkpython/",
             "--bump", "minor", "--ref", "refs/heads/main", "--json-output", str(json_output),
             "--github-output", str(github_output)), cwd=self.repository, capture_output=True, text=True, check=False
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("version=v0.1.0", github_output.read_text(encoding="utf-8"))
        self.assertEqual(self.git("tag", "--list", "sdkpython/*"), "")

    def test_plan_can_repair_a_reachable_older_sdk_tag(self) -> None:
        package = self.repository / "sdkpython"
        package.mkdir()
        manifest = package / "pyproject.toml"
        manifest.write_text('[project]\nname = "dex-connectors-sdk"\nversion = "0.1.0"\n', encoding="utf-8")
        self.commit("sdkpython: initial release")
        original_sha = self.git("rev-parse", "HEAD")
        self.git("tag", "sdkpython/v0.1.0")
        manifest.write_text('[project]\nname = "dex-connectors-sdk"\nversion = "0.2.0"\n', encoding="utf-8")
        self.commit("sdkpython: second release")
        self.git("tag", "sdkpython/v0.2.0")
        self.enter_repository()
        self.git("checkout", "--detach", original_sha)
        plan = release.create_plan("sdkpython", "sdkpython/", target_version="v0.1.0")
        self.assertEqual(plan.tag, "sdkpython/v0.1.0")
        self.assertEqual(plan.source_sha, original_sha)

    def test_first_typescript_sdk_release_uses_declared_0_1_0(self) -> None:
        package = self.repository / "sdkts"
        package.mkdir()
        (package / "package.json").write_text(json.dumps({
            "name": "@superdurable/connectors-sdk", "version": "0.1.0",
            "repository": {"type": "git", "url": NPM_REPOSITORY_URL, "directory": "sdkts"},
        }), encoding="utf-8")
        self.commit("sdkts: add SDK")
        self.enter_repository()
        plan = release.create_plan("sdkts", "sdkts/", bump="minor")
        self.assertEqual(plan.version, "v0.1.0")
        self.assertEqual(plan.tag, "sdkts/v0.1.0")
        self.assertEqual(plan.module_path, "@superdurable/connectors-sdk")
        self.assertEqual(self.git("tag", "--list", "sdkts/*"), "")

    def test_typescript_release_rejects_private_package(self) -> None:
        package = self.repository / "sdkts"
        package.mkdir()
        (package / "package.json").write_text(
            '{"name":"@superdurable/connectors-sdk","version":"0.1.0","private":true}', encoding="utf-8"
        )
        self.commit("sdkts: add private SDK")
        self.enter_repository()
        with self.assertRaisesRegex(ValueError, "private and cannot be published"):
            release.create_plan("sdkts", "sdkts/", bump="minor")

    def test_typescript_release_plan_requires_matching_repository_for_provenance(self) -> None:
        package = self.repository / "sdkts"
        package.mkdir()
        package_json = package / "package.json"
        self.enter_repository()
        for repository in (None, {"type": "git", "url": "git+https://github.com/other/repo.git",
                                  "directory": "sdkts"},
                           {"type": "git", "url": NPM_REPOSITORY_URL, "directory": "other"},
                           {"type": "hg", "url": NPM_REPOSITORY_URL, "directory": "sdkts"}):
            with self.subTest(repository=repository):
                package_json.write_text(json.dumps({"name": "@superdurable/connectors-sdk", "version": "0.1.0",
                                                    "repository": repository}), encoding="utf-8")
                with self.assertRaisesRegex(ValueError, "repository must declare.*npm provenance"):
                    release.create_plan("sdkts", "sdkts/", bump="minor")

    def test_language_release_waits_for_completed_go_release(self) -> None:
        workflow = (Path(__file__).resolve().parents[2] / ".github/workflows/release-connector.yml").read_text(
            encoding="utf-8"
        )
        build_job = workflow.split("  build-language:\n", 1)[1].split("  publish-language:\n", 1)[0]
        publish_job = workflow.split("  publish-language:\n", 1)[1]
        self.assertIn("needs: [plan, release]", build_job)
        self.assertIn("needs.release.result == 'success' || needs.release.result == 'skipped'", build_job)
        self.assertIn("validation_args+=(--dry-run)", build_job)
        self.assertIn("needs: [plan, release, build-language]", publish_job)
        self.assertIn("needs.build-language.result == 'success'", publish_job)
        makefile = (Path(__file__).resolve().parents[2] / "Makefile").read_text(encoding="utf-8")
        self.assertIn('--language python --allow-pending-go-release', makefile)
        self.assertIn('--language typescript --allow-pending-go-release', makefile)
        self.assertNotIn('--allow-pending-go-release', build_job)

    def test_publish_language_rejects_build_skip_without_complete_release(self) -> None:
        workflow = (Path(__file__).resolve().parents[2] / ".github/workflows/release-connector.yml").read_text(
            encoding="utf-8"
        )
        publish_job = workflow.split("  publish-language:\n", 1)[1]
        state_step = publish_job.split("      - name: Inspect language release state\n", 1)[1].split(
            "      - name:", 1
        )[0]
        state_script = textwrap.dedent(state_step.split("        run: |\n", 1)[1])
        runner_temp = self.repository / "runner"
        (runner_temp / "language-work").mkdir(parents=True)
        (runner_temp / "language-work/skip").touch()
        github_output = self.repository / "github-output"
        environment = dict(os.environ, RUNNER_TEMP=str(runner_temp), GITHUB_OUTPUT=str(github_output),
                           TAG_PREFIX="connectors/newsapi/python/", RELEASE_VERSION="v0.1.0",
                           GITHUB_SHA=self.git("rev-parse", "HEAD"))
        failed = subprocess.run(("bash", "-e", "-o", "pipefail", "-c", state_script), cwd=self.repository,
                                env=environment, capture_output=True, text=True, check=False)
        self.assertNotEqual(failed.returncode, 0)
        self.assertIn("without a completed release", failed.stderr)
        self.assertFalse(github_output.exists())
        (runner_temp / "language-work/completed-release").touch()
        complete = subprocess.run(("bash", "-e", "-o", "pipefail", "-c", state_script), cwd=self.repository,
                                  env=environment, capture_output=True, text=True, check=False)
        self.assertEqual(complete.returncode, 0, complete.stderr)
        self.assertIn("release_required=false", github_output.read_text(encoding="utf-8"))

    def test_github_output_rejects_control_characters(self) -> None:
        plan = release.ReleasePlan("sdkpython", "dex-connectors-sdk\nunsafe=1", "", "", "minor",
                                   "v0.1.0", "sdkpython/v0.1.0", "abc", ())
        with self.assertRaisesRegex(ValueError, "control characters"):
            release.write_github_output(self.repository / "output", plan)

    def test_language_release_manifest_records_artifact_digest(self) -> None:
        artifact = self.repository / "package.tgz"
        with tarfile.open(artifact, "w:gz") as archive:
            package_json = b'{"name":"@superdurable/connector-newsapi","version":"0.1.0"}'
            member = tarfile.TarInfo("package/package.json")
            member.size = len(package_json)
            archive.addfile(member, io.BytesIO(package_json))
            connector_manifest = b'{"releaseTag":"connectors/newsapi/go/v0.20.0","package":{"ecosystem":"npm","name":"@superdurable/connector-newsapi","version":"0.1.0"}}'
            member = tarfile.TarInfo("package/dex-connector.json")
            member.size = len(connector_manifest)
            archive.addfile(member, io.BytesIO(connector_manifest))
        output = self.repository / "language-release.json"
        release.write_language_release_manifest(
            output, "typescript", "@superdurable/connector-newsapi", "v0.1.0",
            "connectors/newsapi/typescript/v0.1.0", "abc123", [artifact]
        )
        document = json.loads(output.read_text(encoding="utf-8"))
        self.assertEqual(document["artifacts"]["package.tgz"],
                         hashlib.sha256(artifact.read_bytes()).hexdigest())
        self.assertTrue((self.repository / "package.tgz.sha256").is_file())

    def test_language_artifacts_reject_wrong_tarball_identity_and_extra_files(self) -> None:
        artifact = self.repository / "package.tgz"
        for name, version in (("@superdurable/connectors-sdk", "0.1.0"),
                              ("@superdurable/connector-newsapi", "1.0.0")):
            with tarfile.open(artifact, "w:gz") as archive:
                package_json = json.dumps({"name": name, "version": version}).encode("utf-8")
                member = tarfile.TarInfo("package/package.json")
                member.size = len(package_json)
                archive.addfile(member, io.BytesIO(package_json))
            with self.assertRaisesRegex(ValueError, "differs from planned"):
                release.verify_language_artifacts("typescript", "@superdurable/connector-newsapi", "v0.1.0", [artifact])
        (self.repository / "extra.tgz").write_bytes(b"extra")
        with self.assertRaisesRegex(ValueError, "unexpected files"):
            release.verify_language_artifacts("typescript", "@superdurable/connector-newsapi", "v0.1.0", [artifact])

    def test_npm_artifacts_reject_dual_roots_links_and_duplicate_manifest(self) -> None:
        artifact = self.repository / "package.tgz"
        planned = b'{"name":"@superdurable/connector-newsapi","version":"0.1.0"}'
        substituted = b'{"name":"@superdurable/connectors-sdk","version":"9.9.9"}'
        for second_name, second_content, member_type in (
            ("zzz/package.json", substituted, tarfile.REGTYPE),
            ("package/linked", b"", tarfile.SYMTYPE),
            ("package/hardlink", b"", tarfile.LNKTYPE),
            ("package/../other", substituted, tarfile.REGTYPE),
            ("/package/other", substituted, tarfile.REGTYPE),
            ("package/package.json", substituted, tarfile.REGTYPE),
        ):
            with tarfile.open(artifact, "w:gz") as archive:
                first = tarfile.TarInfo("package/package.json")
                first.size = len(planned)
                archive.addfile(first, io.BytesIO(planned))
                second = tarfile.TarInfo(second_name)
                second.type = member_type
                second.size = len(second_content)
                if member_type in (tarfile.SYMTYPE, tarfile.LNKTYPE):
                    second.linkname = "package/package.json"
                archive.addfile(second, io.BytesIO(second_content))
            with self.assertRaises(ValueError, msg=second_name):
                release.verify_language_artifacts("typescript", "@superdurable/connector-newsapi", "v0.1.0", [artifact])

    def test_python_artifacts_reject_ambiguous_wheel_and_sdist(self) -> None:
        wheel = self.repository / "dex_connector_newsapi-0.1.0-py3-none-any.whl"
        sdist = self.repository / "dex_connector_newsapi-0.1.0.tar.gz"
        metadata = b"Name: dex-connector-newsapi\nVersion: 0.1.0\n"
        with zipfile.ZipFile(wheel, "w") as archive:
            archive.writestr("dex_connector_newsapi-0.1.0.dist-info/METADATA", metadata)
            archive.writestr("dex_connectors_sdk-9.9.9.dist-info/WHEEL", b"Wheel-Version: 1.0\n")
        with tarfile.open(sdist, "w:gz") as archive:
            member = tarfile.TarInfo("dex_connector_newsapi-0.1.0/PKG-INFO")
            member.size = len(metadata)
            archive.addfile(member, io.BytesIO(metadata))
        with self.assertRaises(ValueError):
            release.verify_language_artifacts("python", "dex-connector-newsapi", "v0.1.0", [wheel, sdist])
        with zipfile.ZipFile(wheel, "w") as archive:
            archive.writestr("dex_connector_newsapi-0.1.0.dist-info/METADATA", metadata)
        with tarfile.open(sdist, "w:gz") as archive:
            for root in ("dex_connector_newsapi-0.1.0", "dex_connectors_sdk-9.9.9"):
                member = tarfile.TarInfo(f"{root}/PKG-INFO")
                member.size = len(metadata)
                archive.addfile(member, io.BytesIO(metadata))
        with self.assertRaises(ValueError):
            release.verify_language_artifacts("python", "dex-connector-newsapi", "v0.1.0", [wheel, sdist])

    def test_pypi_verification_rejects_different_registry_digest(self) -> None:
        artifact = self.repository / "dex_connector_newsapi-0.1.0.tar.gz"
        artifact.write_bytes(b"local artifact")
        response = io.BytesIO(json.dumps({"urls": [{"filename": artifact.name,
                                                     "digests": {"sha256": "different"}}]}).encode("utf-8"))
        with mock.patch.object(release.urllib.request, "urlopen", return_value=response):
            with self.assertRaisesRegex(ValueError, "different SHA256"):
                release.verify_pypi_artifacts("dex-connector-newsapi", "v0.1.0", [artifact])

    def test_pypi_post_publish_waits_for_404_then_verifies_digest(self) -> None:
        artifact = self.repository / "dex_connectors_sdk-0.1.0.tar.gz"
        artifact.write_bytes(b"first SDK release")
        missing = urllib.error.HTTPError("https://pypi.org", 404, "Not Found", None, None)
        self.addCleanup(missing.close)
        published = io.BytesIO(json.dumps({"urls": [{"filename": artifact.name, "digests": {
            "sha256": hashlib.sha256(artifact.read_bytes()).hexdigest()
        }}]}).encode("utf-8"))
        with mock.patch.object(release.urllib.request, "urlopen", side_effect=[missing, published]) as urlopen, \
             mock.patch.object(release.time, "sleep") as sleep:
            release.verify_pypi_artifacts("dex-connectors-sdk", "v0.1.0", [artifact], require_all=True,
                                          wait_seconds=120)
        self.assertEqual(urlopen.call_count, 2)
        sleep.assert_called_once()

    def test_pypi_post_publish_stops_at_visibility_deadline(self) -> None:
        artifact = self.repository / "dex_connectors_sdk-0.1.0.tar.gz"
        artifact.write_bytes(b"first SDK release")
        missing = urllib.error.HTTPError("https://pypi.org", 404, "Not Found", None, None)
        self.addCleanup(missing.close)
        with mock.patch.object(release.urllib.request, "urlopen", side_effect=missing) as urlopen, \
             mock.patch.object(release.time, "monotonic", side_effect=[100, 100, 101]), \
             mock.patch.object(release.time, "sleep") as sleep:
            with self.assertRaisesRegex(ValueError, "within 1s"):
                release.verify_pypi_artifacts("dex-connectors-sdk", "v0.1.0", [artifact], require_all=True,
                                              wait_seconds=1)
        urlopen.assert_called_once()
        sleep.assert_not_called()

    def test_pypi_post_publish_retries_transient_lookup_failures(self) -> None:
        artifact = self.repository / "dex_connectors_sdk-0.1.0.tar.gz"
        artifact.write_bytes(b"first SDK release")
        unavailable = urllib.error.HTTPError("https://pypi.org", 503, "Unavailable", None, None)
        self.addCleanup(unavailable.close)
        published = io.BytesIO(json.dumps({"urls": [{"filename": artifact.name, "digests": {
            "sha256": hashlib.sha256(artifact.read_bytes()).hexdigest()
        }}]}).encode("utf-8"))
        with mock.patch.object(release.urllib.request, "urlopen", side_effect=[unavailable,
             urllib.error.URLError("temporary connection failure"), TimeoutError("read timed out"), published]) as urlopen, \
             mock.patch.object(release.time, "sleep") as sleep:
            release.verify_pypi_artifacts("dex-connectors-sdk", "v0.1.0", [artifact], require_all=True,
                                          wait_seconds=120)
        self.assertEqual(urlopen.call_count, 4)
        self.assertEqual(sleep.call_count, 3)

    def test_python_artifacts_reject_wrong_version(self) -> None:
        wheel = self.repository / "dex_connector_newsapi-0.1.0-py3-none-any.whl"
        with zipfile.ZipFile(wheel, "w") as archive:
            archive.writestr("dex_connector_newsapi-0.1.0.dist-info/METADATA", "Name: dex-connector-newsapi\nVersion: 9.9.9\n")
        sdist = self.repository / "dex_connector_newsapi-0.1.0.tar.gz"
        with tarfile.open(sdist, "w:gz") as archive:
            metadata = b"Name: dex-connector-newsapi\nVersion: 0.1.0\n"
            member = tarfile.TarInfo("dex_connector_newsapi-0.1.0/PKG-INFO")
            member.size = len(metadata)
            archive.addfile(member, io.BytesIO(metadata))
        with self.assertRaisesRegex(ValueError, "differs from planned"):
            release.verify_language_artifacts("python", "dex-connector-newsapi", "v0.1.0", [wheel, sdist])

    @mock.patch.object(release, "read_declared_language_capabilities", return_value=(set(), set()))
    def test_language_package_requires_released_registry_sdk_without_local_overrides(self, _capabilities: mock.Mock) -> None:
        python_package = self.repository / "connectors/newsapi/python"
        python_package.mkdir(parents=True)
        (python_package.parent / "connector.yaml").write_text("apiVersion: connectors.dex.dev/v1alpha1\nspec:\n  languages:\n    python: {version: v0.1.0, operations: [], triggers: []}\n", encoding="utf-8")
        manifest_directory = python_package / "src/dex_connectors/newsapi"
        manifest_directory.mkdir(parents=True)
        (manifest_directory / "dex-connector.json").write_text(json.dumps({
            "releaseTag": "connectors/newsapi/go/v0.1.0",
            "package": {"ecosystem": "pypi", "name": "dex-connector-newsapi", "version": "0.1.0"},
            "factories": [],
        }), encoding="utf-8")
        pyproject = python_package / "pyproject.toml"
        pyproject.write_text('[project]\nname = "dex-connector-newsapi"\nversion = "0.1.0"\ndependencies = ["dex-connectors-sdk==0.1.0"]\n', encoding="utf-8")
        lookup = mock.Mock(spec=release.DependencyReleaseLookup)
        lookup.has_tag.return_value = True
        lookup.is_tag_reachable_from_main.return_value = True
        lookup.release_asset_names.side_effect = lambda tag: (
            frozenset({"language-release.json"}) if tag.startswith("sdkpython/") else
            frozenset({"connector-release.complete"})
        )
        lookup.is_language_sdk_published.return_value = True
        self.assertEqual(release.validate_language_package(str(python_package), "python", lookup), "sdkpython/v0.1.0")
        lookup.is_language_sdk_published.return_value = False
        with self.assertRaisesRegex(ValueError, "not published in the registry"):
            release.validate_language_package(str(python_package), "python", lookup)
        pyproject.write_text(pyproject.read_text(encoding="utf-8") + '[tool.uv.sources]\ndex-connectors-sdk = { path = "../../../sdkpython" }\n', encoding="utf-8")
        with self.assertRaisesRegex(ValueError, "sources overrides"):
            release.validate_language_package(str(python_package), "python", lookup)
        wrong_directory = python_package / "src/dex_connectors/wrong_package"
        wrong_directory.mkdir()
        (manifest_directory / "dex-connector.json").rename(wrong_directory / "dex-connector.json")
        with self.assertRaisesRegex(ValueError, "must contain generated"):
            release.validate_language_connector_release(python_package, "python", lookup)

    def test_python_sdk_requirement_accepts_floor_and_legacy_exact_pin_only(self) -> None:
        self.assertEqual(release.parse_python_sdk_requirement("p", ["dex-connectors-sdk>=0.7.0,<1", "httpx>=0.27"]),
                         release.PythonSDKRequirement("0.7.0", is_floor=True))
        self.assertEqual(release.parse_python_sdk_requirement("p", ["dex-connectors-sdk==0.1.0"]),
                         release.PythonSDKRequirement("0.1.0", is_floor=False))
        for dependencies in (
            ["dex-connectors-sdk>=0.7.0"],
            ["dex-connectors-sdk>=0.7.0,<2"],
            ["dex-connectors-sdk>=0.7.0,<1.0"],
            ["dex-connectors-sdk>=0.7.0, <1"],
            ["dex-connectors-sdk >=0.7.0,<1"],
            ["dex-connectors-sdk>0.7.0,<1"],
            ["dex-connectors-sdk~=0.7"],
            ["dex-connectors-sdk>=0.8.0rc1,<1"],
            ["dex-connectors-sdk==0.8.0rc1"],
            ["dex-connectors-sdk>=1.0.0,<1"],
            ["dex-connectors-sdk>=0.07.0,<1"],
            ["dex-connectors-sdk>=0.7.0,<1; python_version >= '3.11'"],
            ["dex-connectors-sdk[extra]>=0.7.0,<1"],
            ["dex_connectors_sdk>=0.7.0,<1"],
            ["Dex.Connectors-SDK==0.7.0"],
            ["dex-connectors-sdk @ file:///sdkpython"],
            ["dex-connectors-sdk>=0.7.0,<1", "dex-connectors-sdk==0.7.0"],
            ["dex-connectors-sdk-extras==0.7.0"],
            [],
        ):
            with self.subTest(dependencies=dependencies):
                with self.assertRaisesRegex(ValueError, "must require dex-connectors-sdk"):
                    release.parse_python_sdk_requirement("p", dependencies)

    @mock.patch.object(release, "read_declared_language_capabilities", return_value=(set(), set()))
    def test_python_floor_requirement_requires_uv_lock_at_the_floor(self, _capabilities: mock.Mock) -> None:
        package = self.repository / "connectors/newsapi/python"
        package.mkdir(parents=True)
        (package.parent / "connector.yaml").write_text("metadata:\n  version: v0.23.0\n", encoding="utf-8")
        (package / "pyproject.toml").write_text(
            '[project]\nname = "dex-connector-newsapi"\nversion = "0.2.0"\n'
            'dependencies = ["dex-connectors-sdk>=0.7.0,<1"]\n[dependency-groups]\ndev = ["pytest>=8"]\n', encoding="utf-8")
        connector_manifest = release.language_connector_manifest_path(package, "python")
        connector_manifest.parent.mkdir(parents=True)
        connector_manifest.write_text(json.dumps({
            "releaseTag": "connectors/newsapi/go/v0.23.0",
            "package": {"ecosystem": "pypi", "name": "dex-connector-newsapi", "version": "0.2.0"},
            "factories": [],
        }), encoding="utf-8")
        lookup = mock.Mock(spec=release.DependencyReleaseLookup)
        lookup.has_tag.return_value = True
        lookup.is_tag_reachable_from_main.return_value = True
        lookup.release_asset_names.side_effect = lambda tag, allow_missing=False: (
            frozenset({"language-release.json"}) if tag.startswith("sdkpython/") else
            frozenset({"connector-release.complete"})
        )
        lookup.is_language_sdk_published.return_value = True

        with self.assertRaisesRegex(ValueError, "must contain uv.lock"):
            release.validate_language_package(str(package), "python", lookup)
        lock = package / "uv.lock"
        lock.write_text('version = 1\n[[package]]\nname = "dex-connectors-sdk"\nversion = "0.8.0"\n', encoding="utf-8")
        with self.assertRaisesRegex(ValueError, r"lock dex-connectors-sdk at its floor 0\.7\.0, found \['0\.8\.0'\]"):
            release.validate_language_package(str(package), "python", lookup)
        lock.write_text('version = 1\n[[package]]\nname = "dex-connectors-sdk"\nversion = "0.7.0"\n', encoding="utf-8")
        self.assertEqual(release.validate_language_package(str(package), "python", lookup), "sdkpython/v0.7.0")
        lookup.is_language_sdk_published.assert_called_with("python", "0.7.0")

    @mock.patch.object(release, "read_declared_language_capabilities", return_value=(set(), set()))
    def test_language_connector_requires_completed_go_release(self, _capabilities: mock.Mock) -> None:
        package = self.repository / "connectors/newsapi/typescript"
        package.mkdir(parents=True)
        (package.parent / "connector.yaml").write_text("apiVersion: connectors.dex.dev/v1alpha1\nspec:\n  languages:\n    typescript: {version: v0.1.0, operations: [], triggers: []}\n", encoding="utf-8")
        (package / "package.json").write_text(json.dumps({
            "name": "@superdurable/connector-newsapi", "version": "0.1.0",
        }), encoding="utf-8")
        (package / "dex-connector.json").write_text(json.dumps({
            "releaseTag": "connectors/newsapi/go/v0.20.0",
            "package": {"ecosystem": "npm", "name": "@superdurable/connector-newsapi", "version": "0.1.0"},
            "factories": [],
        }), encoding="utf-8")
        lookup = mock.Mock(spec=release.DependencyReleaseLookup)
        lookup.has_tag.return_value = False
        with self.assertRaisesRegex(ValueError, "Go connector release tag is missing"):
            release.validate_language_connector_release(package, "typescript", lookup)
        release.validate_language_connector_release(package, "typescript", lookup, require_completed_release=False)
        lookup.has_tag.return_value = True
        lookup.is_tag_reachable_from_main.return_value = False
        with self.assertRaisesRegex(ValueError, "not reachable from main"):
            release.validate_language_connector_release(package, "typescript", lookup)
        lookup.is_tag_reachable_from_main.return_value = True
        lookup.release_asset_names.return_value = frozenset()
        with self.assertRaisesRegex(ValueError, "connector-release.complete"):
            release.validate_language_connector_release(package, "typescript", lookup)
        lookup.release_asset_names.return_value = frozenset({"connector-release.complete"})
        release.validate_language_connector_release(package, "typescript", lookup)
        with mock.patch.object(release, "read_declared_language_capabilities", return_value=({"getMessage"}, set())):
            with self.assertRaisesRegex(ValueError, "declared operations differ"):
                release.validate_language_connector_release(package, "typescript", lookup)

    @mock.patch.object(release, "read_declared_language_capabilities", return_value=(set(), set()))
    def test_language_ci_accepts_only_current_pending_go_release(self, _capabilities: mock.Mock) -> None:
        for language in ("python", "typescript"):
            with self.subTest(language=language):
                package = self.repository / "connectors/newsapi" / language
                package.mkdir(parents=True, exist_ok=True)
                (package.parent / "connector.yaml").write_text("metadata:\n  version: v0.23.0\n", encoding="utf-8")
                if language == "python":
                    (package / "pyproject.toml").write_text(
                        '[project]\nname = "dex-connector-newsapi"\nversion = "0.1.0"\n'
                        'dependencies = ["dex-connectors-sdk==0.1.0"]\n', encoding="utf-8")
                    sdk_tag = "sdkpython/v0.1.0"
                    package_name = "dex-connector-newsapi"
                    ecosystem = "pypi"
                else:
                    (package / "package.json").write_text(json.dumps({
                        "name": "@superdurable/connector-newsapi", "version": "0.1.0",
                        "repository": {"type": "git", "url": NPM_REPOSITORY_URL,
                                       "directory": "connectors/newsapi/typescript"},
                        "dependencies": {"@superdurable/connectors-sdk": "0.1.0"},
                    }), encoding="utf-8")
                    sdk_tag = "sdkts/v0.1.0"
                    package_name = "@superdurable/connector-newsapi"
                    ecosystem = "npm"
                connector_manifest = release.language_connector_manifest_path(package, language)
                connector_manifest.parent.mkdir(parents=True, exist_ok=True)
                document = {"releaseTag": "connectors/newsapi/go/v0.23.0",
                            "package": {"ecosystem": ecosystem, "name": package_name, "version": "0.1.0"},
                            "factories": []}
                connector_manifest.write_text(json.dumps(document), encoding="utf-8")
                lookup = mock.Mock(spec=release.DependencyReleaseLookup)
                lookup.has_tag.side_effect = lambda tag: tag == sdk_tag
                lookup.is_tag_reachable_from_main.return_value = True
                lookup.release_asset_names.side_effect = lambda tag, allow_missing=False: (
                    frozenset({"language-release.json"}) if tag == sdk_tag else frozenset()
                )
                lookup.is_language_sdk_published.return_value = True

                self.assertEqual(release.validate_language_package(str(package), language, lookup,
                                                                    allow_pending_go_release=True), sdk_tag)
                with self.assertRaisesRegex(ValueError, "Go connector release tag is missing"):
                    release.validate_language_package(str(package), language, lookup)

                lookup.has_tag.side_effect = lambda tag: True
                self.assertEqual(release.validate_language_package(str(package), language, lookup,
                                                                    allow_pending_go_release=True), sdk_tag)
                with self.assertRaisesRegex(ValueError, "connector-release.complete"):
                    release.validate_language_package(str(package), language, lookup)

                lookup.release_asset_names.side_effect = lambda tag, allow_missing=False: (
                    frozenset({"language-release.json"}) if tag == sdk_tag
                    else frozenset({"connector-release.complete"})
                )
                self.assertEqual(release.validate_language_package(str(package), language, lookup), sdk_tag)
                lookup.is_tag_reachable_from_main.side_effect = lambda tag: tag == sdk_tag
                with self.assertRaisesRegex(ValueError, "not reachable from main"):
                    release.validate_language_package(str(package), language, lookup,
                                                      allow_pending_go_release=True)
                lookup.is_tag_reachable_from_main.side_effect = None
                lookup.is_tag_reachable_from_main.return_value = True

                document["releaseTag"] = "connectors/newsapi/go/v0.22.0"
                connector_manifest.write_text(json.dumps(document), encoding="utf-8")
                with self.assertRaisesRegex(ValueError, "must equal current Go connector tag"):
                    release.validate_language_package(str(package), language, lookup,
                                                      allow_pending_go_release=True)
                document["releaseTag"] = "connectors/other/go/v0.23.0"
                connector_manifest.write_text(json.dumps(document), encoding="utf-8")
                with self.assertRaisesRegex(ValueError, "must name its Go connector release"):
                    release.validate_language_package(str(package), language, lookup,
                                                      allow_pending_go_release=True)

    def test_missing_release_lookup_is_allowed_only_in_pending_mode(self) -> None:
        lookup = release.DependencyReleaseLookup("origin/main")
        with mock.patch.object(release, "release_assets", return_value=None):
            self.assertEqual(lookup.release_asset_names("connectors/newsapi/go/v0.23.0", allow_missing=True), frozenset())
            with self.assertRaisesRegex(ValueError, "cannot read the GitHub release"):
                lookup.release_asset_names("connectors/newsapi/go/v0.23.0")
        with mock.patch.object(release, "release_assets", side_effect=RuntimeError("GitHub API rate limit (HTTP 403)")):
            with self.assertRaisesRegex(ValueError, "rate limit"):
                lookup.release_asset_names("connectors/newsapi/go/v0.23.0", allow_missing=True)

    def test_language_capability_coverage_rejects_missing_factories_and_triggers(self) -> None:
        source_manifest = self.repository / "connector.yaml"
        source_manifest.write_text("apiVersion: connectors.dex.dev/v1alpha1\n", encoding="utf-8")
        document = {"factories": []}
        with mock.patch.object(release, "read_declared_language_capabilities", return_value=({"getMessage"}, set())):
            with self.assertRaisesRegex(ValueError, "declared operations differ"):
                release.validate_language_capability_coverage(source_manifest, "python", document)
        document["factories"] = [{"operationId": "getMessage"}]
        with mock.patch.object(release, "read_declared_language_capabilities", return_value=({"getMessage"}, set())):
            release.validate_language_capability_coverage(source_manifest, "python", document)
        with mock.patch.object(release, "read_declared_language_capabilities", return_value=({"getMessage"}, {"messageReceived"})):
            with self.assertRaisesRegex(ValueError, "Trigger factories are not supported"):
                release.validate_language_capability_coverage(source_manifest, "python", document)

    def test_connectorctl_reads_language_capabilities(self) -> None:
        manifest = SCRIPT.parents[2] / "connectors/newsapi/connector.yaml"
        self.assertEqual(release.read_declared_language_capabilities(manifest, "python"), ({"searchArticles"}, set()))

    def test_connector_archives_require_matching_dex_manifest(self) -> None:
        npm_asset = self.repository / "package.tgz"
        npm_identity = b'{"name":"@superdurable/connector-newsapi","version":"0.1.0"}'
        def write_npm(connector_version: str | None) -> None:
            with tarfile.open(npm_asset, "w:gz") as archive:
                member = tarfile.TarInfo("package/package.json")
                member.size = len(npm_identity)
                archive.addfile(member, io.BytesIO(npm_identity))
                if connector_version is not None:
                    contents = json.dumps({"releaseTag": "connectors/newsapi/go/v0.20.0",
                                           "package": {"ecosystem": "npm", "name": "@superdurable/connector-newsapi",
                                                       "version": connector_version}}).encode("utf-8")
                    member = tarfile.TarInfo("package/dex-connector.json")
                    member.size = len(contents)
                    archive.addfile(member, io.BytesIO(contents))
        write_npm(None)
        with self.assertRaisesRegex(ValueError, "dex-connector.json"):
            release.verify_language_artifacts("typescript", "@superdurable/connector-newsapi", "v0.1.0", [npm_asset])
        write_npm("9.9.9")
        with self.assertRaisesRegex(ValueError, "package identity"):
            release.verify_language_artifacts("typescript", "@superdurable/connector-newsapi", "v0.1.0", [npm_asset])
        write_npm("0.1.0")
        release.verify_language_artifacts("typescript", "@superdurable/connector-newsapi", "v0.1.0", [npm_asset])
        npm_asset.unlink()

        wheel = self.repository / "dex_connector_newsapi-0.1.0-py3-none-any.whl"
        sdist = self.repository / "dex_connector_newsapi-0.1.0.tar.gz"
        metadata = b"Name: dex-connector-newsapi\nVersion: 0.1.0\n"
        connector_manifest = json.dumps({"releaseTag": "connectors/newsapi/go/v0.20.0",
                                         "package": {"ecosystem": "pypi", "name": "dex-connector-newsapi",
                                                       "version": "0.1.0"}}).encode("utf-8")
        def write_python(include_wheel: bool, include_sdist: bool, import_package: str = "newsapi") -> None:
            with zipfile.ZipFile(wheel, "w") as archive:
                archive.writestr("dex_connector_newsapi-0.1.0.dist-info/METADATA", metadata)
                if include_wheel:
                    archive.writestr(f"dex_connectors/{import_package}/dex-connector.json", connector_manifest)
            with tarfile.open(sdist, "w:gz") as archive:
                member = tarfile.TarInfo("dex_connector_newsapi-0.1.0/PKG-INFO")
                member.size = len(metadata)
                archive.addfile(member, io.BytesIO(metadata))
                if include_sdist:
                    member = tarfile.TarInfo(f"dex_connector_newsapi-0.1.0/src/dex_connectors/{import_package}/dex-connector.json")
                    member.size = len(connector_manifest)
                    archive.addfile(member, io.BytesIO(connector_manifest))
        write_python(False, True)
        with self.assertRaisesRegex(ValueError, "wheel must contain one dex-connector.json"):
            release.verify_language_artifacts("python", "dex-connector-newsapi", "v0.1.0", [wheel, sdist])
        write_python(True, False)
        with self.assertRaisesRegex(ValueError, "package archive must contain one dex-connector.json"):
            release.verify_language_artifacts("python", "dex-connector-newsapi", "v0.1.0", [wheel, sdist])
        write_python(True, True)
        release.verify_language_artifacts("python", "dex-connector-newsapi", "v0.1.0", [wheel, sdist])
        write_python(True, True, "wrong_package")
        with self.assertRaisesRegex(ValueError, "dex-connector.json must be at"):
            release.verify_language_artifacts("python", "dex-connector-newsapi", "v0.1.0", [wheel, sdist])

    def test_candidate_build_stamps_packaged_manifest_version(self) -> None:
        for language, relative_path, expected in (
            ("python", "src/dex_connectors/newsapi/dex-connector.json", "0.1.0rc2"),
            ("typescript", "dex-connector.json", "0.1.0-rc.2"),
        ):
            with self.subTest(language=language):
                package = self.repository / "connectors/newsapi" / language
                manifest = package / relative_path
                manifest.parent.mkdir(parents=True, exist_ok=True)
                manifest.write_text(json.dumps({"package": {"version": "0.1.0"}}), encoding="utf-8")
                release.stamp_language_candidate_connector_manifest(package, language, "v0.1.0-rc.2")
                self.assertEqual(json.loads(manifest.read_text(encoding="utf-8"))["package"]["version"], expected)
                with self.assertRaisesRegex(ValueError, "package.version must equal"):
                    release.stamp_language_candidate_connector_manifest(package, language, "v0.1.0-rc.2")

    def test_typescript_package_rejects_file_and_workspace_dependencies(self) -> None:
        typescript_package = self.repository / "connectors/newsapi/typescript"
        typescript_package.mkdir(parents=True)
        package_json = typescript_package / "package.json"
        for dependency in ("file:../../../sdkts", "workspace:*"):
            package_json.write_text(json.dumps({
                "repository": {"type": "git", "url": NPM_REPOSITORY_URL,
                               "directory": "connectors/newsapi/typescript"},
                "dependencies": {"@superdurable/connectors-sdk": dependency},
            }), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "path, file, git, or workspace"):
                release.validate_language_package(str(typescript_package), "typescript", mock.Mock())

    def test_typescript_connector_validator_requires_matching_repository(self) -> None:
        package = self.repository / "connectors/newsapi/typescript"
        package.mkdir(parents=True)
        package_json = package / "package.json"
        for repository in (None, {"type": "git", "url": NPM_REPOSITORY_URL, "directory": "sdkts"}):
            with self.subTest(repository=repository):
                package_json.write_text(json.dumps({
                    "repository": repository,
                    "dependencies": {"@superdurable/connectors-sdk": "0.1.0"},
                }), encoding="utf-8")
                with self.assertRaisesRegex(ValueError, "repository must declare.*npm provenance"):
                    release.validate_language_package(str(package), "typescript", mock.Mock())

    def test_language_commits_include_shared_manifest_changes(self) -> None:
        package = self.repository / "connectors/newsapi/python"
        package.mkdir(parents=True)
        manifest = package.parent / "connector.yaml"
        manifest.write_text("version: v0.1.0\n", encoding="utf-8")
        (package / "pyproject.toml").write_text('[project]\nname = "dex-connector-newsapi"\nversion = "0.1.0"\n', encoding="utf-8")
        self.commit("connector(newsapi): add Python package")
        self.git("tag", "connectors/newsapi/python/v0.1.0")
        manifest.write_text("version: v0.2.0\n", encoding="utf-8")
        self.commit("connector(newsapi): change shared manifest (breaking)")
        self.enter_repository()
        commits = release.component_commits("connectors/newsapi/python", "connectors/newsapi/python/v0.1.0")
        self.assertEqual(commits, (self.git("rev-parse", "HEAD"),))
        plan = release.ReleasePlan(
            component_path="connectors/newsapi/python", module_path="dex-connector-newsapi",
            baseline_tag="connectors/newsapi/python/v0.1.0", baseline_version="v0.1.0",
            bump="patch", version="v0.1.1", tag="connectors/newsapi/python/v0.1.1",
            source_sha=commits[0], commits=commits,
        )
        _, is_breaking = release.release_notes(plan, "")
        self.assertTrue(is_breaking)

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

    def test_first_declared_version_is_a_minor_release(self) -> None:
        self.assertEqual(release.version_bump("", "v0.1.0"), "minor")
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

    def test_connector_release_title_names_language(self) -> None:
        self.assertEqual(release.connector_release_title("Zoom", "go", "v0.24.0"), "Zoom Go v0.24.0")
        self.assertEqual(release.connector_release_title("Stripe", "python", "v0.1.0"), "Stripe Python v0.1.0")
        self.assertEqual(release.connector_release_title("Stripe", "typescript", "v0.1.0"), "Stripe TypeScript v0.1.0")
        self.assertEqual(release.connector_release_title("Stripe", "go", "v0.25.0-rc.1"), "Stripe Go v0.25.0-rc.1")
        self.assertEqual(release.connector_release_title("Google Sheets", "go", "v1.0.0"), "Google Sheets Go v1.0.0")

    def test_connector_release_title_rejects_invalid_input(self) -> None:
        for display_name, language, version in (("Zoom", "rust", "v0.1.0"), ("Zoom", "Go", "v0.1.0"),
                                                ("Zoom", "go", "0.1.0"), ("", "go", "v0.1.0"),
                                                ("Zoom\n", "go", "v0.1.0x"), ("Zo\nom", "go", "v0.1.0")):
            with self.assertRaises(ValueError, msg=(display_name, language, version)):
                release.connector_release_title(display_name, language, version)

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

    def test_language_descriptor_only_migration_does_not_break_patch_release(self) -> None:
        for language in ("python", "typescript"):
            with self.subTest(language=language):
                package = self.repository / f"connectors/google/gmail/{language}"
                package.mkdir(parents=True, exist_ok=True)
                if language == "python":
                    (package / "pyproject.toml").write_text(
                        '[project]\nname = "dex-connector-google-gmail"\nversion = "0.1.0"\n', encoding="utf-8"
                    )
                    descriptor = package / "src/dex_connectors/google_gmail/dex-connector.json"
                else:
                    (package / "package.json").write_text(json.dumps({
                        "name": "@superdurable/connector-google-gmail", "version": "0.1.0",
                        "repository": {"type": "git", "url": NPM_REPOSITORY_URL,
                                       "directory": "connectors/google/gmail/typescript"},
                    }), encoding="utf-8")
                    descriptor = package / "dex-connector.json"
                descriptor.parent.mkdir(parents=True, exist_ok=True)
                descriptor.write_text('{"goModule": "old"}\n', encoding="utf-8")
                self.commit(f"connector(gmail): add {language} package")
                self.git("tag", f"connectors/google/gmail/{language}/v0.1.0")
                migration_branch = f"migration-{language}"
                self.git("switch", "-c", migration_branch)
                descriptor.write_text('{"goModule": "new"}\n', encoding="utf-8")
                self.commit("tooling: rewrite Go connector module paths (breaking)")
                self.git("switch", "main")
                self.git("merge", "--no-ff", migration_branch, "-m", "tooling: move Go connectors (breaking)")
                if language == "python":
                    package_manifest = package / "pyproject.toml"
                    package_manifest.write_text(package_manifest.read_text().replace("0.1.0", "0.1.1"), encoding="utf-8")
                else:
                    package_manifest = package / "package.json"
                    package_manifest.write_text(package_manifest.read_text().replace("0.1.0", "0.1.1"), encoding="utf-8")
                self.commit(f"connector(gmail): fix {language} package")
                self.enter_repository()
                plan = release.create_plan(str(package.relative_to(self.repository)),
                                           f"connectors/google/gmail/{language}/", target_version="v0.1.1")
                notes, is_breaking = release.release_notes(plan, "")
                self.assertFalse(is_breaking)
                self.assertIn("tooling: move Go connectors", notes.split("## Changes\n\n", 1)[1])
                release.validate_breaking_bump(plan, is_breaking)

    def test_language_source_change_keeps_breaking_marker(self) -> None:
        package = self.repository / "connectors/google/gmail/python"
        source = package / "src/dex_connectors/google_gmail"
        source.mkdir(parents=True)
        (package / "pyproject.toml").write_text(
            '[project]\nname = "dex-connector-google-gmail"\nversion = "0.1.0"\n', encoding="utf-8"
        )
        descriptor = source / "dex-connector.json"
        descriptor.write_text('{"goModule": "old"}\n', encoding="utf-8")
        (source / "client.py").write_text("def call(): pass\n", encoding="utf-8")
        self.commit("connector(gmail): add Python package")
        self.git("tag", "connectors/google/gmail/python/v0.1.0")
        descriptor.write_text('{"goModule": "new"}\n', encoding="utf-8")
        (source / "client.py").write_text("def call(): return None\n", encoding="utf-8")
        self.commit("connector(gmail): change Python API (breaking)")
        (package / "pyproject.toml").write_text(
            '[project]\nname = "dex-connector-google-gmail"\nversion = "0.1.1"\n', encoding="utf-8"
        )
        self.commit("connector(gmail): bump Python patch")
        self.enter_repository()
        plan = release.create_plan("connectors/google/gmail/python", "connectors/google/gmail/python/",
                                   target_version="v0.1.1")
        notes, is_breaking = release.release_notes(plan, "")
        self.assertTrue(is_breaking)
        self.assertIn("connector(gmail): change Python API", notes.split("## Changes", 1)[0])
        with self.assertRaisesRegex(ValueError, "cannot use a patch"):
            release.validate_breaking_bump(plan, is_breaking)

    def test_release_notes_include_each_pull_request_once(self) -> None:
        plan = release.ReleasePlan(
            component_path="sdkgo", module_path=SDK_MODULE, baseline_tag="", baseline_version="",
            bump="minor", version="v0.2.0", tag="sdkgo/v0.2.0", source_sha="latest",
            commits=("first", "second", "third"),
        )
        pull = {"title": "tooling: move Go connector modules", "body": "", "html_url": "https://github.com/superdurable/dex-connectors-library/pull/369",
                "user": {"login": "longquanzheng"}}
        with mock.patch.object(release, "associated_pull_request", return_value=pull), \
                mock.patch.object(release, "commit_message", side_effect=("first change", "second change", "third change (breaking)")):
            notes, is_breaking = release.release_notes(plan, "superdurable/dex-connectors-library")
        self.assertTrue(is_breaking)
        self.assertEqual(notes.count("/pull/369"), 1)
        self.assertIn("## Breaking Changes\n\n- [tooling: move Go connector modules]", notes)

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
        inventory = self.repository / "release-inventory.json"
        inventory.write_text(json.dumps({"connectors/openai/v0.7.0": [
            "connector-release.json", "connector-release.complete"]}), encoding="utf-8")
        lookup = release.DependencyReleaseLookup("main")
        with mock.patch.dict(os.environ, {"GH_RELEASE_INVENTORY_PATH": str(inventory)}):
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

    def test_go_subdirectory_plan_uses_old_tag_and_allows_one_time_v1_minor(self) -> None:
        old_module = self.connector()
        self.git("tag", "connectors/openai/v1.2.0")
        go_module = old_module / "go"
        go_module.mkdir()
        (old_module / "go.mod").rename(go_module / "go.mod")
        (old_module / "connector.go").rename(go_module / "connector.go")
        go_mod = go_module / "go.mod"
        go_mod.write_text(go_mod.read_text().replace("example.com/connectors/openai", f"{LIBRARY}/connectors/openai/go"))
        self.commit("tooling: move Go connector modules (breaking)")
        self.enter_repository()

        plan = release.create_plan("connectors/openai/go", "connectors/openai/go/", target_version="v1.3.0")
        self.assertEqual(plan.baseline_tag, "connectors/openai/v1.2.0")
        self.assertEqual(plan.tag, "connectors/openai/go/v1.3.0")
        self.assertEqual(plan.module_path, f"{LIBRARY}/connectors/openai/go")
        notes, is_breaking = release.release_notes(plan, "")
        self.assertIn(f"Moved from `{LIBRARY}/connectors/openai`", notes)
        self.assertTrue(is_breaking)
        release.validate_breaking_bump(plan, is_breaking)
        skipped = release.create_plan("connectors/openai/go", "connectors/openai/go/", target_version="v1.4.0")
        with self.assertRaisesRegex(ValueError, "v1\\+ breaking release requires a major bump"):
            release.validate_breaking_bump(skipped, True)

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


class ScopedBreakingMarkerTest(unittest.TestCase):
    """Covers per-language "(breaking: <language>)" markers next to plain "(breaking)"."""

    HACKERNEWS_COMPONENTS = {
        "go": "connectors/hackernews/go",
        "python": "connectors/hackernews/python",
        "typescript": "connectors/hackernews/typescript",
    }

    def patch_plan(self, component_path: str, commits: tuple[str, ...] = ("change",)) -> release.ReleasePlan:
        return release.ReleasePlan(
            component_path=component_path, module_path="module", baseline_tag=f"{component_path}/v0.24.0",
            baseline_version="v0.24.0", bump="patch", version="v0.24.1", tag=f"{component_path}/v0.24.1",
            source_sha=commits[-1], commits=commits,
        )

    def notes_for(self, component_path: str, pulls: list[dict[str, object] | None], messages: list[str]) -> tuple[str, bool]:
        plan = self.patch_plan(component_path, tuple(f"commit-{index}" for index in range(len(messages))))
        with mock.patch.object(release, "associated_pull_request", side_effect=pulls), \
                mock.patch.object(release, "commit_message", side_effect=messages), \
                mock.patch.object(release, "commit_changes_beyond_language_descriptor", return_value=True):
            return release.release_notes(plan, "superdurable/dex-connectors-library")

    def assert_patch_allowed(self, language: str, pulls: list[dict[str, object] | None], messages: list[str],
                             is_patch_allowed: bool) -> None:
        component_path = self.HACKERNEWS_COMPONENTS[language]
        _, is_breaking = self.notes_for(component_path, pulls, messages)
        plan = self.patch_plan(component_path)
        if is_patch_allowed:
            self.assertFalse(is_breaking, language)
            release.validate_breaking_bump(plan, is_breaking)
        else:
            self.assertTrue(is_breaking, language)
            with self.assertRaisesRegex(ValueError, "cannot use a patch"):
                release.validate_breaking_bump(plan, is_breaking)

    @staticmethod
    def pull(title: str, body: str = "", number: int = 676) -> dict[str, object]:
        return {"title": title, "body": body, "user": {"login": "contributor"},
                "html_url": f"https://github.com/superdurable/dex-connectors-library/pull/{number}"}

    def test_typescript_scoped_marker_allows_go_and_python_patches(self) -> None:
        title = "connector(hackernews): make listStoryIDs feed and limit optional (breaking: typescript)"
        for language, is_patch_allowed in (("go", True), ("python", True), ("typescript", False)):
            with self.subTest(language=language):
                self.assert_patch_allowed(language, [self.pull(title)], ["squash"], is_patch_allowed)

    def test_plain_marker_rejects_every_language_patch(self) -> None:
        title = "connector(hackernews): change shared manifest (breaking)"
        for language in self.HACKERNEWS_COMPONENTS:
            with self.subTest(language=language):
                self.assert_patch_allowed(language, [self.pull(title)], ["squash"], False)

    def test_multi_language_scope_names_each_listed_language(self) -> None:
        body = "Changes the Python and TypeScript input types (breaking: python, typescript)."
        for language, is_patch_allowed in (("go", True), ("python", False), ("typescript", False)):
            with self.subTest(language=language):
                self.assert_patch_allowed(language, [self.pull("connector(hackernews): change input", body)],
                                          ["squash"], is_patch_allowed)

    def test_scoped_marker_in_direct_commit_message(self) -> None:
        message = "connector(hackernews): change Go API\n\n(breaking: go)"
        for language, is_patch_allowed in (("go", False), ("python", True), ("typescript", True)):
            with self.subTest(language=language):
                self.assert_patch_allowed(language, [None], [message], is_patch_allowed)

    def test_plain_and_scoped_markers_together_mean_every_language(self) -> None:
        for language in self.HACKERNEWS_COMPONENTS:
            with self.subTest(language=language, case="one pull request"):
                self.assert_patch_allowed(
                    language, [self.pull("connector(hackernews): change API (breaking: typescript)", "(breaking)")],
                    ["squash"], False,
                )
            with self.subTest(language=language, case="separate commits"):
                self.assert_patch_allowed(
                    language,
                    [self.pull("connector(hackernews): change TypeScript (breaking: typescript)", number=1),
                     self.pull("connector(hackernews): change everything (breaking)", number=2)],
                    ["first", "second"], False,
                )

    def test_scoped_markers_across_commits_union_their_languages(self) -> None:
        pulls = [self.pull("connector(hackernews): change Python (breaking: python)", number=1),
                 self.pull("connector(hackernews): change TypeScript (breaking: typescript)", number=2)]
        for language, is_patch_allowed in (("go", True), ("python", False), ("typescript", False)):
            with self.subTest(language=language):
                self.assert_patch_allowed(language, pulls, ["first", "second"], is_patch_allowed)

    def test_unmarked_language_lists_scoped_change_under_changes(self) -> None:
        title = "connector(hackernews): make listStoryIDs feed and limit optional (breaking: typescript)"
        go_notes, _ = self.notes_for("connectors/hackernews/go", [self.pull(title)], ["squash"])
        typescript_notes, _ = self.notes_for("connectors/hackernews/typescript", [self.pull(title)], ["squash"])
        entry = "- [connector(hackernews): make listStoryIDs feed and limit optional](https://github.com/superdurable/dex-connectors-library/pull/676) by @contributor"
        self.assertEqual(go_notes, f"## Breaking Changes\n\nNone.\n\n## Changes\n\n{entry}\n")
        self.assertEqual(typescript_notes, f"## Breaking Changes\n\n{entry}\n\n## Changes\n\nNone.\n")

    def test_sdk_components_use_their_language(self) -> None:
        for component_path, language in (("sdkgo", "go"), ("sdkpython", "python"), ("sdkts", "typescript")):
            for marked_language in ("go", "python", "typescript"):
                with self.subTest(component_path=component_path, marked_language=marked_language):
                    _, is_breaking = self.notes_for(
                        component_path, [None], [f"sdk: change API (breaking: {marked_language})"],
                    )
                    self.assertEqual(is_breaking, marked_language == language)

    def test_language_descriptor_only_change_ignores_scoped_marker(self) -> None:
        plan = self.patch_plan("connectors/hackernews/typescript")
        with mock.patch.object(release, "commit_message", return_value="tooling: rewrite descriptors (breaking: typescript)"), \
                mock.patch.object(release, "commit_changes_beyond_language_descriptor", return_value=False):
            _, is_breaking = release.release_notes(plan, "")
        self.assertFalse(is_breaking)

    def test_marked_breaking_languages(self) -> None:
        self.assertEqual(release.marked_breaking_languages("no marker", "breaking change", "(breaking changes)",
                                                           "Breaking Changes: none"), frozenset())
        self.assertEqual(release.marked_breaking_languages("x (breaking)"), frozenset({"go", "python", "typescript"}))
        self.assertEqual(release.marked_breaking_languages("x (breaking: typescript)"), frozenset({"typescript"}))
        self.assertEqual(release.marked_breaking_languages("(breaking: go, python)"), frozenset({"go", "python"}))
        self.assertEqual(release.marked_breaking_languages("(breaking: go, python, typescript)"),
                         frozenset({"go", "python", "typescript"}))
        self.assertEqual(release.marked_breaking_languages("(breaking: go)", "(breaking: python)"),
                         frozenset({"go", "python"}))

    def test_invalid_scoped_marker_fails_release_notes(self) -> None:
        for text in ("(breaking: ts)", "(breaking: TypeScript)", "(breaking: )", "(breaking:)",
                     "(breaking: go,)", "(breaking: go python)", "(breaking: go", "(breaking) and (breaking: rust)",
                     "(breaking : typescript)", "( breaking: typescript)", "(Breaking: typescript)",
                     "(BREAKING)", "(Breaking)", "( breaking )", "(breaking )", "(breaking:typescript)",
                     "(breaking:  typescript)", "(breaking: typescript )", "(breaking: go,python)",
                     "(breaking: go , python)", "(breaking: go,  python)", "(breaking:\ngo)",
                     "( Breaking : go", "(breaking: <language>[, <language>...])"):
            with self.subTest(text=text):
                with self.assertRaisesRegex(ValueError, "invalid breaking marker"):
                    release.marked_breaking_languages(text)
        with self.assertRaisesRegex(ValueError, "invalid breaking marker"):
            self.notes_for("connectors/hackernews/go", [self.pull("connector(hackernews): change", "(breaking: ts)")],
                           ["squash"])

    def test_non_canonical_marker_names_canonical_syntax(self) -> None:
        with self.assertRaisesRegex(ValueError, re.escape("invalid breaking marker '(breaking : typescript)': write "
                                                          "(breaking) or (breaking: <language>[, <language>...])")):
            release.marked_breaking_languages("connector(hackernews): change (breaking : typescript)")
        with self.assertRaisesRegex(ValueError, "missing its closing"):
            release.marked_breaking_languages("( Breaking : typescript and more")

    def test_markers_inside_markdown_code_are_documentation(self) -> None:
        pull_request_681_body = (
            "- New scoped marker `(breaking: <language>[, <language>...])` with `go`, `python`, `typescript`; "
            "the v0 \"breaking cannot be a patch\" rule applies only to the named packages. Plain `(breaking)` "
            "still applies to all, and wins when both appear."
        )
        self.assertEqual(release.marked_breaking_languages(pull_request_681_body), frozenset())
        for text in (
            "write ``(breaking: <language>)`` or ``a ` (breaking: ts)``",
            "a span ```(Breaking : go)``` here",
            "```\n(breaking: <language>[, <language>...])\n(breaking)\n```\nafter the fence",
            "~~~markdown\n(breaking : go)\n~~~",
            "   ```text\n(BREAKING)\n   ``````\n",
            "an unclosed fence\n````\n(breaking: ts)\n```\nstill code",
            "the span `(breaking:` hides an unclosed marker",
        ):
            with self.subTest(text=text):
                self.assertEqual(release.marked_breaking_languages(text), frozenset())
        self.assertEqual(release.marked_breaking_languages("`(breaking)` documented; this changes Go (breaking: go)"),
                         frozenset({"go"}))
        self.assertEqual(release.marked_breaking_languages("```\n(breaking)\n```\n(breaking: python)"),
                         frozenset({"python"}))
        for text in ("an unmatched ` backtick (breaking : go)", "```\ncode\n```\n(Breaking)",
                     "``(breaking)` mismatched (breaking: ts)"):
            with self.subTest(text=text):
                with self.assertRaisesRegex(ValueError, "invalid breaking marker"):
                    release.marked_breaking_languages(text)

    def test_pull_request_681_squash_commit_releases_no_breaking_change(self) -> None:
        body = "- New scoped marker `(breaking: <language>[, <language>...])` with `go`, `python`, `typescript`."
        message = ("tooling: scope breaking markers to a language package (#681)\n\n"
                   "Add the scoped marker `(breaking: <language>[, <language>...])`; plain `(breaking)` still applies.")
        pull = self.pull("tooling: scope breaking markers to a language package", body, 681)
        for language in ("go", "python", "typescript"):
            with self.subTest(language=language, source="pull request"):
                self.assert_patch_allowed(language, [pull], [message], True)
            with self.subTest(language=language, source="direct commit"):
                self.assert_patch_allowed(language, [None], [message], True)
            with self.subTest(language=language, source="prose marker"):
                self.assert_patch_allowed(language, [None], [message + "\n\nThis is (breaking)."], False)

    def test_release_component_language(self) -> None:
        for component_path, language in (
            ("sdkgo", "go"), ("sdkpython", "python"), ("sdkts", "typescript"),
            ("connectors/google/gmail/go", "go"), ("connectors/google/gmail/python", "python"),
            ("connectors/google/gmail/typescript", "typescript"), ("connectors/zoom", "go"),
        ):
            with self.subTest(component_path=component_path):
                self.assertEqual(release.component_language(component_path), language)


class PythonSDKRegistryLookupTest(unittest.TestCase):
    def test_retries_dropped_connections_and_server_errors(self) -> None:
        unavailable = urllib.error.HTTPError("https://pypi.org", 503, "Unavailable", None, None)
        self.addCleanup(unavailable.close)
        published = mock.MagicMock()
        published.__enter__.return_value.status = 200
        lookup = release.DependencyReleaseLookup("origin/main")
        with mock.patch.object(release.urllib.request, "urlopen", side_effect=[
                http.client.RemoteDisconnected("Remote end closed connection without response"), unavailable, published]) as urlopen, \
             mock.patch.object(release.time, "sleep") as sleep:
            self.assertTrue(lookup.is_language_sdk_published("python", "0.4.0"))
        self.assertEqual(urlopen.call_count, 3)
        self.assertEqual(sleep.call_count, 2)

    def test_persistent_dropped_connection_fails_after_bounded_attempts(self) -> None:
        lookup = release.DependencyReleaseLookup("origin/main")
        dropped = http.client.RemoteDisconnected("Remote end closed connection without response")
        with mock.patch.object(release.urllib.request, "urlopen", side_effect=dropped) as urlopen, \
             mock.patch.object(release.time, "sleep"):
            with self.assertRaisesRegex(ValueError, "PyPI SDK lookup failed: Remote end closed"):
                lookup.is_language_sdk_published("python", "0.4.0")
        self.assertEqual(urlopen.call_count, release.PYPI_LOOKUP_ATTEMPTS)

    def test_certificate_verification_failure_is_not_retried(self) -> None:
        lookup = release.DependencyReleaseLookup("origin/main")
        rejected = urllib.error.URLError(ssl.SSLCertVerificationError(1, "certificate verify failed"))
        with mock.patch.object(release.urllib.request, "urlopen", side_effect=rejected) as urlopen, \
             mock.patch.object(release.time, "sleep") as sleep:
            with self.assertRaisesRegex(ValueError, "certificate verify failed"):
                lookup.is_language_sdk_published("python", "0.4.0")
        urlopen.assert_called_once()
        sleep.assert_not_called()

    def test_npm_lookup_retries_network_errors_only(self) -> None:
        lookup = release.DependencyReleaseLookup("origin/main")

        def npm(returncode: int, stdout: str = "", stderr: str = "") -> subprocess.CompletedProcess[str]:
            return subprocess.CompletedProcess(["npm"], returncode, stdout=stdout, stderr=stderr)

        cases = (
            ("reset then published", [npm(1, stderr="npm error code ECONNRESET\n"), npm(1, stderr="npm error code E503\n"),
                                      npm(0, stdout='"0.4.0"')], True, 3),
            ("missing", [npm(1, stderr="npm error code E404\n")], False, 1),
            ("forbidden", [npm(1, stderr="npm error code E403\n")], "npm SDK lookup failed: npm error code E403", 1),
            ("persistent timeout", [npm(1, stderr="npm error code ETIMEDOUT\n")] * release.NPM_LOOKUP_ATTEMPTS,
             "ETIMEDOUT", release.NPM_LOOKUP_ATTEMPTS),
        )
        for name, results, expectation, attempts in cases:
            with self.subTest(case=name), \
                    mock.patch.object(release.subprocess, "run", side_effect=results) as run, \
                    mock.patch.object(release.time, "sleep"):
                if isinstance(expectation, str):
                    with self.assertRaisesRegex(ValueError, expectation):
                        lookup.is_language_sdk_published("typescript", "0.4.0")
                else:
                    self.assertEqual(expectation, lookup.is_language_sdk_published("typescript", "0.4.0"))
                self.assertEqual(attempts, run.call_count)

    def test_missing_version_and_client_errors_are_not_retried(self) -> None:
        lookup = release.DependencyReleaseLookup("origin/main")
        for code, expectation in ((404, None), (403, "HTTP 403")):
            with self.subTest(code=code):
                error = urllib.error.HTTPError("https://pypi.org", code, "Refused", None, None)
                self.addCleanup(error.close)
                with mock.patch.object(release.urllib.request, "urlopen", side_effect=error) as urlopen, \
                     mock.patch.object(release.time, "sleep") as sleep:
                    if expectation is None:
                        self.assertFalse(lookup.is_language_sdk_published("python", "0.4.0"))
                    else:
                        with self.assertRaisesRegex(ValueError, expectation):
                            lookup.is_language_sdk_published("python", "0.4.0")
                urlopen.assert_called_once()
                sleep.assert_not_called()


if __name__ == "__main__":
    unittest.main()
