# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

from __future__ import annotations

import importlib.util
import io
import hashlib
import json
import os
import re
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
        fake_bin = self.repository / "bin"
        fake_bin.mkdir()
        fake_gh = fake_bin / "gh"
        fake_gh.write_text("#!/bin/sh\nprintf '%s\\n' \"${RELEASE_ASSETS}\"\n", encoding="utf-8")
        fake_gh.chmod(0o755)
        github_output = self.repository / "github-output"
        environment = dict(os.environ, RUNNER_TEMP=str(runner_temp), GITHUB_OUTPUT=str(github_output),
                           TAG_PREFIX="connectors/newsapi/python/", RELEASE_VERSION="v0.1.0",
                           GITHUB_SHA=self.git("rev-parse", "HEAD"))
        environment["PATH"] = f"{fake_bin}{os.pathsep}{environment['PATH']}"
        environment["RELEASE_ASSETS"] = "language-release.json"
        failed = subprocess.run(("bash", "-e", "-o", "pipefail", "-c", state_script), cwd=self.repository,
                                env=environment, capture_output=True, text=True, check=False)
        self.assertNotEqual(failed.returncode, 0)
        self.assertIn("without a completed release", failed.stderr)
        self.assertFalse(github_output.exists())
        environment["RELEASE_ASSETS"] = "language-release.complete"
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

    def test_language_package_requires_released_registry_sdk_without_local_overrides(self) -> None:
        python_package = self.repository / "connectors/newsapi/python"
        python_package.mkdir(parents=True)
        pyproject = python_package / "pyproject.toml"
        pyproject.write_text('[project]\nname = "dex-connector-newsapi"\nversion = "0.1.0"\ndependencies = ["dex-connectors-sdk>=0.1.0"]\n', encoding="utf-8")
        lookup = mock.Mock(spec=release.DependencyReleaseLookup)
        lookup.has_tag.return_value = True
        lookup.is_tag_reachable_from_main.return_value = True
        lookup.release_asset_names.return_value = frozenset({"language-release.json"})
        lookup.is_language_sdk_published.return_value = True
        self.assertEqual(release.validate_language_package(str(python_package), "python", lookup), "sdkpython/v0.1.0")
        lookup.is_language_sdk_published.return_value = False
        with self.assertRaisesRegex(ValueError, "not published in the registry"):
            release.validate_language_package(str(python_package), "python", lookup)
        pyproject.write_text(pyproject.read_text(encoding="utf-8") + '[tool.uv.sources]\ndex-connectors-sdk = { path = "../../../sdkpython" }\n', encoding="utf-8")
        with self.assertRaisesRegex(ValueError, "sources overrides"):
            release.validate_language_package(str(python_package), "python", lookup)

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
