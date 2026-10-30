# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

import hashlib
import io
import os
import re
import shutil
import subprocess
import tarfile
import tempfile
import unittest
import urllib.error
from pathlib import Path
from unittest.mock import patch

from script.dex_compatibility import (
    CONNECTOR_SDK_DIRECTORY,
    CREDENTIAL_ISOLATION_FIXTURE,
    CREDENTIAL_ISOLATION_MODULE_PATH,
    GO_VET_BUILD_TAG_SETS,
    GITHUB_REQUEST_ATTEMPTS,
    GITHUB_RATE_LIMIT_MAX_WAIT_SECONDS,
    connector_releases,
    create_module_proxy,
    dexcli_archive_name,
    dex_go_sdk_version_from_environment,
    dex_release_from_environment,
    download_dexcli,
    dex_web_compatibility_sources,
    dex_web_compatibility_test_names,
    download,
    download_released_artifacts,
    example_consumer_name,
    fixture_module_path,
    flow_examples,
    install_dexcli,
    manifests,
    rate_limit_wait,
    resolve_dex_release,
    stable_connector_tags,
    stable_cli_tags,
    write_dex_version_plan,
    parse_connector_directories,
    require_dex_web_security_tests_passed,
    require_dex_web_tests_passed,
)

ROOT = Path(__file__).resolve().parents[1]


class ModuleProxyTest(unittest.TestCase):
    def test_synthetic_version_changes_with_module_contents(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            module = root / "connector"
            module.mkdir()
            (module / "go.mod").write_text("module example.com/connector\n\ngo 1.24\n")
            source = module / "connector.go"
            source.write_text("package connector\n\nconst Version = 1\n")

            _, first = create_module_proxy(module, root / "proxy-one")
            source.write_text("package connector\n\nconst Version = 2\n")
            _, second = create_module_proxy(module, root / "proxy-two")

            self.assertRegex(first, re.compile(r"^v0\.0\.[1-9][0-9]*$"))
            self.assertNotEqual(first, second)


class DexCliInstallTest(unittest.TestCase):
    def test_installs_bundled_server_and_legacy_cli_archives(self) -> None:
        for has_bundled_server in (False, True):
            with self.subTest(has_bundled_server=has_bundled_server), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                archive_name = dexcli_archive_name("v1.26.1")
                source_archive = root / "source.tar.gz"
                with tarfile.open(source_archive, "w:gz") as archive:
                    files = {"package/dexcli": b"cli"}
                    if has_bundled_server:
                        files["server/dex-server-zd"] = b"server"
                    for name, contents in files.items():
                        member = tarfile.TarInfo(name)
                        member.size = len(contents)
                        member.mode = 0o755
                        archive.addfile(member, io.BytesIO(contents))
                source_checksums = root / "source-checksums.txt"
                source_checksums.write_text(f"{hashlib.sha256(source_archive.read_bytes()).hexdigest()}  {archive_name}\n")
                assets = {"archive": source_archive, "checksums": source_checksums}

                def copy_asset(url: str, output: Path) -> None:
                    shutil.copy2(assets[url], output)

                release = {
                    "tag_name": "cli-v1.26.1",
                    "assets": [
                        {"name": archive_name, "browser_download_url": "archive"},
                        {"name": "checksums.txt", "browser_download_url": "checksums"},
                    ],
                }
                extraction = root / "extracted"
                extraction.mkdir()
                with patch("script.dex_compatibility.download", side_effect=copy_asset):
                    binary = download_dexcli(release, extraction)
                output = root / "installed" / "dexcli"
                install_dexcli(binary, output)

                self.assertEqual(b"cli", output.read_bytes())
                bundled_server = output.with_name("dex-server-zd")
                self.assertEqual(has_bundled_server, bundled_server.exists())
                if has_bundled_server:
                    self.assertEqual(b"server", bundled_server.read_bytes())

    def test_pre_resolved_digest_is_checked_without_downloading_checksums_again(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive_name = dexcli_archive_name("v1.26.1")
            source_archive = root / "source.tar.gz"
            with tarfile.open(source_archive, "w:gz") as archive:
                member = tarfile.TarInfo("dexcli")
                member.size = 3
                archive.addfile(member, io.BytesIO(b"cli"))
            release = {"tag_name": "cli-v1.26.1", "assets": [{"name": archive_name, "browser_download_url": "archive"}]}

            def copy_asset(url: str, output: Path) -> None:
                self.assertEqual(url, "archive")
                shutil.copy2(source_archive, output)

            extraction = root / "extract"
            extraction.mkdir()
            with patch("script.dex_compatibility.download", side_effect=copy_asset) as download_asset:
                binary = download_dexcli(release, extraction, hashlib.sha256(source_archive.read_bytes()).hexdigest())
            self.assertEqual(binary.read_bytes(), b"cli")
            download_asset.assert_called_once()
            failed_extraction = root / "extract-again"
            failed_extraction.mkdir()
            with patch("script.dex_compatibility.download", side_effect=copy_asset):
                with self.assertRaisesRegex(RuntimeError, "Dex CLI checksum mismatch"):
                    download_dexcli(release, failed_extraction, "0" * 64)


class DexVersionPlanTest(unittest.TestCase):
    def test_plan_exports_one_cli_asset_and_one_go_sdk_version(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output = root / "github-output"
            archive_name = dexcli_archive_name("v1.26.1")
            base_url = "https://github.com/superdurable/dex/releases/download/cli-v1.26.1/"
            release = {"tag_name": "cli-v1.26.1", "assets": [
                {"name": archive_name, "browser_download_url": base_url + archive_name},
                {"name": "checksums.txt", "browser_download_url": base_url + "checksums.txt"},
            ]}

            def copy_checksums(url: str, destination: Path) -> None:
                self.assertEqual(url, base_url + "checksums.txt")
                destination.write_text(f"{'a' * 64}  {archive_name}\n")

            with patch("script.dex_compatibility.resolve_dex_release", return_value=release) as resolve, \
                    patch("script.dex_compatibility.download", side_effect=copy_checksums) as download_asset, \
                    patch("script.dex_compatibility.latest_stable_go_module_version", return_value="v1.42.0") as sdk_version:
                write_dex_version_plan(root, "v1.26.1", output)
            resolve.assert_called_once_with("v1.26.1")
            download_asset.assert_called_once()
            sdk_version.assert_called_once()
            self.assertEqual(output.read_text(),
                f"cli_tag=cli-v1.26.1\ncli_asset_url={base_url + archive_name}\ncli_sha256={'a' * 64}\ngo_sdk_version=v1.42.0\n")

    def test_plan_rejects_an_asset_url_outside_direct_github_releases(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            archive_name = dexcli_archive_name("v1.26.1")
            release = {"tag_name": "cli-v1.26.1", "assets": [
                {"name": archive_name, "browser_download_url": "https://api.github.com/assets/123"},
                {"name": "checksums.txt", "browser_download_url": "https://github.com/superdurable/dex/releases/download/cli-v1.26.1/checksums.txt"},
            ]}
            with patch("script.dex_compatibility.resolve_dex_release", return_value=release), \
                    patch("script.dex_compatibility.download") as download_asset:
                with self.assertRaisesRegex(RuntimeError, "unexpected download URL"):
                    write_dex_version_plan(ROOT, "latest", Path(temporary) / "github-output")
            download_asset.assert_not_called()

    def test_shards_use_the_plan_without_resolving_versions_again(self) -> None:
        archive_name = dexcli_archive_name("v1.26.1")
        environment = {
            "DEX_CLI_RELEASE_TAG": "cli-v1.26.1",
            "DEX_CLI_ASSET_URL": f"https://github.com/superdurable/dex/releases/download/cli-v1.26.1/{archive_name}",
            "DEX_CLI_SHA256": "a" * 64,
            "DEX_GO_SDK_VERSION": "v1.42.0",
        }
        with patch.dict(os.environ, environment), \
                patch("script.dex_compatibility.resolve_dex_release") as resolve, \
                patch("script.dex_compatibility.latest_stable_go_module_version") as latest_sdk:
            release, digest = dex_release_from_environment("v1.26.1")
            sdk_version = dex_go_sdk_version_from_environment(ROOT)
        self.assertEqual((release["tag_name"], digest, sdk_version), ("cli-v1.26.1", "a" * 64, "v1.42.0"))
        resolve.assert_not_called()
        latest_sdk.assert_not_called()

    def test_incomplete_or_mismatched_plan_fails_closed(self) -> None:
        with patch.dict(os.environ, {"DEX_CLI_RELEASE_TAG": "cli-v1.26.1"}, clear=True):
            with self.assertRaisesRegex(RuntimeError, "must all be set"):
                dex_release_from_environment("latest")
        archive_name = dexcli_archive_name("v1.26.1")
        environment = {
            "DEX_CLI_RELEASE_TAG": "cli-v1.26.1",
            "DEX_CLI_ASSET_URL": f"https://github.com/superdurable/dex/releases/download/cli-v1.26.1/{archive_name}",
            "DEX_CLI_SHA256": "a" * 64,
        }
        with patch.dict(os.environ, environment, clear=True):
            with self.assertRaisesRegex(RuntimeError, "does not match requested"):
                dex_release_from_environment("v1.26.2")

    def test_invalid_plan_values_are_rejected(self) -> None:
        archive_name = dexcli_archive_name("v1.26.1")
        valid = {
            "DEX_CLI_RELEASE_TAG": "cli-v1.26.1",
            "DEX_CLI_ASSET_URL": f"https://github.com/superdurable/dex/releases/download/cli-v1.26.1/{archive_name}",
            "DEX_CLI_SHA256": "a" * 64,
        }
        for key, value, message in (
            ("DEX_CLI_RELEASE_TAG", "cli-v1.26.1-rc.1", "invalid pre-resolved Dex CLI tag"),
            ("DEX_CLI_ASSET_URL", "https://api.github.com/assets/123", "invalid pre-resolved Dex CLI asset URL"),
            ("DEX_CLI_SHA256", "not-a-digest", "invalid pre-resolved Dex CLI SHA-256"),
        ):
            with self.subTest(key=key), patch.dict(os.environ, {**valid, key: value}, clear=True):
                with self.assertRaisesRegex(RuntimeError, message):
                    dex_release_from_environment("latest")
        with patch.dict(os.environ, {"DEX_GO_SDK_VERSION": ""}, clear=True):
            with self.assertRaisesRegex(RuntimeError, "plan output is empty"):
                dex_go_sdk_version_from_environment(ROOT)

    def test_without_a_plan_uses_the_existing_latest_resolvers(self) -> None:
        release = {"tag_name": "cli-v1.26.1"}
        with patch.dict(os.environ, {}, clear=True), \
                patch("script.dex_compatibility.resolve_dex_release", return_value=release) as resolve, \
                patch("script.dex_compatibility.latest_stable_go_module_version", return_value="v1.42.0") as latest_sdk:
            self.assertEqual(dex_release_from_environment("latest"), (release, None))
            self.assertEqual(dex_go_sdk_version_from_environment(ROOT), "v1.42.0")
        resolve.assert_called_once_with("latest")
        latest_sdk.assert_called_once()


class ExampleConsumerNameTest(unittest.TestCase):
    def test_same_example_name_in_two_connectors_gets_distinct_consumers(self) -> None:
        meta = example_consumer_name("connectors/meta", Path("connectors/meta/examples/summarize-text/flow"))
        claude = example_consumer_name("connectors/anthropic", Path("connectors/anthropic/examples/summarize-text/flow"))
        self.assertEqual("meta-summarize-text", meta)
        self.assertEqual("anthropic-summarize-text", claude)

    def test_nested_company_directories_are_part_of_the_name(self) -> None:
        name = example_consumer_name("connectors/google/gemini", Path("x/examples/generate-summary/flow"))
        self.assertEqual("google-gemini-generate-summary", name)


class ConnectorSelectionTest(unittest.TestCase):
    def test_selected_manifests_keep_requested_order(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for directory in ("connectors/acme/chat", "connectors/acme/mail"):
                connector = root / directory
                connector.mkdir(parents=True)
                (connector / "connector.yaml").write_text("manifest")

            selected = parse_connector_directories(root, "connectors/acme/mail,connectors/acme/chat")

            self.assertEqual(
                [root / "connectors/acme/mail/connector.yaml", root / "connectors/acme/chat/connector.yaml"],
                manifests(root, selected),
            )

    def test_empty_selection_has_no_manifests(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)

            self.assertEqual([], manifests(root, parse_connector_directories(root, "")))

    def test_flow_examples_only_include_selected_connectors(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for directory in ("connectors/acme/chat", "connectors/acme/mail"):
                flow = root / directory / "examples" / "example" / "flow"
                flow.mkdir(parents=True)
                (flow / "flow.go").write_text("package flow\n\nfunc GetSteps() {}\n")

            self.assertEqual(
                [root / "connectors/acme/mail/examples/example/flow"],
                flow_examples(root, ["connectors/acme/mail"]),
            )
            self.assertEqual([], flow_examples(root, []))

    def test_released_compatibility_only_includes_selected_connectors(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for directory in ("connectors/acme/chat", "connectors/acme/mail"):
                connector = root / directory
                connector.mkdir(parents=True)
                (connector / "connector.yaml").write_text("manifest")
            tag = "connectors/acme/mail/v0.1.0"
            listed = subprocess.CompletedProcess([], 0, stdout=f"abc\trefs/tags/{tag}\n")
            release = {"tag_name": tag, "assets": [{"name": "connector-release.complete"}]}
            with patch("script.dex_compatibility.run", return_value=listed) as list_tags, \
                    patch("script.dex_compatibility.github_json", return_value=release) as lookup:
                selected = connector_releases(root, None, ["connectors/acme/mail"])

            self.assertEqual([tag], [release["tag_name"] for release in selected])
            self.assertEqual(1, list_tags.call_count)
            self.assertIn("refs/tags/connectors/acme/mail/go/v*", list_tags.call_args.args[0])
            lookup.assert_called_once_with(f"https://api.github.com/repos/superdurable/dex-connectors-library/releases/tags/{tag}")

    def test_latest_completed_release_spans_old_and_go_tag_prefixes(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            connector = root / "connectors/acme/mail"
            connector.mkdir(parents=True)
            (connector / "connector.yaml").write_text("manifest")
            tags = [
                "connectors/acme/mail/v0.5.0",
                "connectors/acme/mail/go/v0.6.0",
                "connectors/acme/mail/v0.7.0",
                "connectors/acme/mail/go/v0.8.0-rc.1",
                "connectors/acme/mail/python/v0.9.0",
            ]
            listed = subprocess.CompletedProcess([], 0, stdout="".join(f"abc\trefs/tags/{tag}\n" for tag in tags))
            requests: list[str] = []

            def lookup(url: str) -> object:
                self.assertIn("/releases/tags/", url)
                requests.append(url)
                tag = url.split("/releases/tags/", 1)[1]
                return {"tag_name": tag, "assets": [] if tag.endswith("v0.7.0") else [{"name": "connector-release.complete"}]}

            with patch("script.dex_compatibility.run", return_value=listed), \
                    patch("script.dex_compatibility.github_json", side_effect=lookup):
                selected = connector_releases(root, None)

            self.assertEqual(["connectors/acme/mail/go/v0.6.0"], [release["tag_name"] for release in selected])
            self.assertEqual(2, len(requests))
            self.assertTrue(all("/releases/tags/" in url for url in requests))

    def test_exact_tag_uses_one_release_lookup_for_pending_publication(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            connector = root / "connectors/acme/mail"
            connector.mkdir(parents=True)
            (connector / "connector.yaml").write_text("manifest")
            tag = "connectors/acme/mail/go/v0.6.0"
            release = {"tag_name": tag, "assets": []}
            with patch("script.dex_compatibility.run") as list_tags, \
                    patch("script.dex_compatibility.github_json", return_value=release) as lookup:
                selected = connector_releases(root, tag)
            self.assertEqual([tag], [item["tag_name"] for item in selected])
            list_tags.assert_not_called()
            lookup.assert_called_once()

    def test_tag_listing_rejects_other_languages_and_prereleases(self) -> None:
        listed = subprocess.CompletedProcess([], 0, stdout=(
            "abc\trefs/tags/connectors/acme/mail/v0.1.0\n"
            "abc\trefs/tags/connectors/acme/mail/go/v0.2.0\n"
            "abc\trefs/tags/connectors/acme/mail/python/v0.3.0\n"
            "abc\trefs/tags/connectors/acme/mail/go/v0.4.0-rc.1\n"
        ))
        with patch("script.dex_compatibility.run", return_value=listed):
            tags = stable_connector_tags(["connectors/acme/mail"])
        self.assertEqual(["connectors/acme/mail/go/v0.2.0", "connectors/acme/mail/v0.1.0"], tags["connectors/acme/mail"])


    def test_tag_listing_keeps_a_connector_directory_named_go(self) -> None:
        listed = subprocess.CompletedProcess([], 0, stdout=(
            "abc\trefs/tags/connectors/acme/v0.1.0\n"
            "abc\trefs/tags/connectors/acme/go/v0.2.0\n"
            "abc\trefs/tags/connectors/acme/go/go/v0.3.0\n"
        ))
        with patch("script.dex_compatibility.run", return_value=listed):
            tags = stable_connector_tags(["connectors/acme", "connectors/acme/go"])
        self.assertEqual(["connectors/acme/v0.1.0"], tags["connectors/acme"])
        self.assertEqual(["connectors/acme/go/go/v0.3.0", "connectors/acme/go/v0.2.0"], tags["connectors/acme/go"])

    def test_missing_latest_release_falls_back_and_logs_each_skip(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for directory in ("connectors/acme/chat", "connectors/acme/mail"):
                connector = root / directory
                connector.mkdir(parents=True)
                (connector / "connector.yaml").write_text("manifest")
            tags = ["connectors/acme/mail/v0.1.0", "connectors/acme/mail/go/v0.2.0"]
            listed = subprocess.CompletedProcess([], 0, stdout="".join(f"abc\trefs/tags/{tag}\n" for tag in tags))

            def lookup(url: str) -> object:
                tag = url.split("/releases/tags/", 1)[1]
                if tag.endswith("v0.2.0"):
                    raise urllib.error.HTTPError(url, 404, "Not Found", {}, None)
                return {"tag_name": tag, "assets": [{"name": "connector-release.complete"}]}

            output = io.StringIO()
            with patch("script.dex_compatibility.run", return_value=listed), \
                    patch("script.dex_compatibility.github_json", side_effect=lookup), \
                    patch("sys.stdout", output):
                selected = connector_releases(root, None)

            self.assertEqual(["connectors/acme/mail/v0.1.0"], [release["tag_name"] for release in selected])
            self.assertIn("Skipping connectors/acme/mail/go/v0.2.0: no published stable release", output.getvalue())
            self.assertIn("No completed release for connectors/acme/chat", output.getvalue())

    def test_exact_tag_outside_the_selected_connectors_or_unstable_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            connector = root / "connectors/acme/mail"
            connector.mkdir(parents=True)
            (connector / "connector.yaml").write_text("manifest")
            for tag in (
                "connectors/acme/chat/go/v0.1.0",
                "connectors/acme/mail/python/v0.1.0",
                "connectors/acme/mail/go/v0.1.0-rc.1",
            ):
                with self.subTest(tag=tag), patch("script.dex_compatibility.github_json") as lookup:
                    with self.assertRaisesRegex(RuntimeError, "published connector release not found"):
                        connector_releases(root, tag)
                    lookup.assert_not_called()

    def test_exact_tag_without_a_release_fails_closed(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            connector = root / "connectors/acme/mail"
            connector.mkdir(parents=True)
            (connector / "connector.yaml").write_text("manifest")
            missing = urllib.error.HTTPError("https://api.github.com", 404, "Not Found", {}, None)
            with patch("script.dex_compatibility.github_json", side_effect=missing):
                with self.assertRaisesRegex(RuntimeError, "published connector release not found: connectors/acme/mail/go/v0.1.0"):
                    connector_releases(root, "connectors/acme/mail/go/v0.1.0")


class ReleasedArtifactLayoutTest(unittest.TestCase):
    def test_both_tag_prefixes_place_metadata_in_the_connector_directory(self) -> None:
        for directory in ("connectors/acme/mail", "connectors/airtable"):
            for tag in (f"{directory}/v0.1.0", f"{directory}/go/v0.2.0"):
                with self.subTest(tag=tag), tempfile.TemporaryDirectory() as temporary:
                    output = Path(temporary)
                    metadata = f'{{"tag": "{tag}", "connectorId": "acme", "sourceSha": "abc"}}'
                    digest = hashlib.sha256(metadata.encode()).hexdigest()
                    contents = {
                        "connector-release.json": metadata,
                        "connector-release.json.sha256": f"{digest}  connector-release.json\n",
                    }
                    release = {
                        "tag_name": tag,
                        "assets": [{"name": name, "browser_download_url": name} for name in contents],
                    }

                    def fake_download(url: str, path: Path) -> None:
                        path.write_text(contents[url])

                    with patch("script.dex_compatibility.download", side_effect=fake_download):
                        download_released_artifacts([release], output)

                    self.assertTrue((output / directory / "connector-release.json").is_file())
                    self.assertFalse((output / directory / "go").exists())
                    pattern = "connectors/*/connector-release.json" if directory.count("/") == 1 else "connectors/*/*/connector-release.json"
                    self.assertEqual([output / directory / "connector-release.json"], list(output.glob(pattern)))


class DexWebCompatibilityTestSelectionTest(unittest.TestCase):
    def test_every_repository_compatibility_test_is_selected(self) -> None:
        names = dex_web_compatibility_test_names(dex_web_compatibility_sources(ROOT))
        self.assertIn("TestExternalConnectorCompatibility", names)
        self.assertIn("TestStudioCommandCredentialIsolation", names)

    def test_only_top_level_test_functions_are_selected(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary) / "fixture_test.go.txt"
            source.write_text(
                "package web\n\n"
                "func TestSelected(t *testing.T) {\n}\n\n"
                "func TestRenamedParameter(test *testing.T) {\n}\n\n"
                "func TestTrailingComment(t *testing.T) { // scenario\n}\n\n"
                "func (scenario *fixtureScenario) TestNotTopLevel(t *testing.T) {\n}\n\n"
                "func testHelper(t *testing.T) {\n}\n\n"
                "func TestMain(m *testing.M) {\n}\n"
            )
            self.assertEqual(
                ["TestSelected", "TestRenamedParameter", "TestTrailingComment"],
                dex_web_compatibility_test_names([source]),
            )

    def test_source_without_top_level_tests_fails(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            selected = Path(temporary) / "selected_test.go.txt"
            selected.write_text("package web\n\nfunc TestSelected(t *testing.T) {\n}\n")
            unselected = Path(temporary) / "unselected_test.go.txt"
            unselected.write_text("package web\n\nfunc testHelper(t *testing.T) {\n}\n")
            with self.assertRaisesRegex(RuntimeError, "unselected_test.go.txt"):
                dex_web_compatibility_test_names([selected, unselected])

    def test_passing_run_is_accepted(self) -> None:
        output = "=== RUN   TestSelected\n--- PASS: TestSelected (0.02s)\nPASS\n"
        require_dex_web_tests_passed(output, ["TestSelected"])

    def test_unselected_test_fails_even_when_go_test_passes(self) -> None:
        output = "testing: warning: no tests to run\nPASS\nok  \tgithub.com/superdurable/dex/web\t0.1s\n"
        with self.assertRaisesRegex(RuntimeError, "TestSelected"):
            require_dex_web_tests_passed(output, ["TestSelected"])

    def test_passing_subtest_does_not_stand_in_for_its_parent(self) -> None:
        output = "    --- PASS: TestSelected/phase (0.01s)\n--- FAIL: TestSelected (0.02s)\n"
        with self.assertRaisesRegex(RuntimeError, "TestSelected"):
            require_dex_web_tests_passed(output, ["TestSelected"])

    def test_security_prefix_is_satisfied_by_a_passing_test_it_selects(self) -> None:
        output = "--- PASS: TestConnectorOAuthUsesPKCE (0.02s)\n--- PASS: TestSlackOAuth (0.01s)\n"
        require_dex_web_security_tests_passed(output, ("TestConnectorOAuth", "TestSlackOAuth"))

    def test_security_prefix_that_selects_nothing_fails(self) -> None:
        output = "--- PASS: TestConnectorOAuthUsesPKCE (0.02s)\n    --- PASS: TestSlackOAuthRenamed/phase (0.01s)\n"
        with self.assertRaisesRegex(RuntimeError, "TestSlackOAuth"):
            require_dex_web_security_tests_passed(output, ("TestConnectorOAuth", "TestSlackOAuth"))


class DexWebFixtureTest(unittest.TestCase):
    def test_credential_isolation_keeps_its_module_path(self) -> None:
        self.assertEqual(CREDENTIAL_ISOLATION_MODULE_PATH, fixture_module_path(CREDENTIAL_ISOLATION_FIXTURE))


class LatestDexGoSdkTest(unittest.TestCase):
    def test_vet_tag_sets_cover_every_build_constraint_in_the_sdk_and_connectors(self) -> None:
        constraint_tags: set[str] = set()
        for directory in (ROOT / CONNECTOR_SDK_DIRECTORY, ROOT / "connectors"):
            for source in directory.rglob("*.go"):
                if "node_modules" in source.parts:
                    continue
                for line in source.read_text(encoding="utf-8").splitlines():
                    if line.startswith("//go:build "):
                        constraint_tags.update(re.findall(r"[A-Za-z_][A-Za-z0-9_.]*", line.removeprefix("//go:build ")))
        self.assertTrue(constraint_tags)
        self.assertLessEqual(constraint_tags, set(GO_VET_BUILD_TAG_SETS) - {""})



class TransientGitHubRetryTest(unittest.TestCase):
    URL = "https://github.com/superdurable/dex/releases/download/cli-v1.5.0/checksums.txt"

    def http_error(self, status: int, headers: dict[str, str] | None = None, body: bytes = b"") -> urllib.error.HTTPError:
        return urllib.error.HTTPError(self.URL, status, "error", headers or {}, io.BytesIO(body))

    def test_download_retries_server_errors_until_the_asset_arrives(self) -> None:
        responses = [self.http_error(503), self.http_error(502), io.BytesIO(b"checksums")]
        with tempfile.TemporaryDirectory() as directory, \
                patch("script.dex_compatibility.urllib.request.urlopen", side_effect=responses) as urlopen, \
                patch("script.dex_compatibility.time.sleep") as sleep:
            output = Path(directory) / "checksums.txt"
            download(self.URL, output)
            self.assertEqual(output.read_bytes(), b"checksums")
        self.assertEqual(urlopen.call_count, 3)
        self.assertEqual([call.args[0] for call in sleep.call_args_list], [2, 4])

    def test_download_fails_once_retries_are_exhausted(self) -> None:
        responses = [self.http_error(503)] * GITHUB_REQUEST_ATTEMPTS
        with tempfile.TemporaryDirectory() as directory, \
                patch("script.dex_compatibility.urllib.request.urlopen", side_effect=responses) as urlopen, \
                patch("script.dex_compatibility.time.sleep"):
            with self.assertRaisesRegex(RuntimeError, "download failed: .*503"):
                download(self.URL, Path(directory) / "checksums.txt")
        self.assertEqual(urlopen.call_count, GITHUB_REQUEST_ATTEMPTS)

    def test_download_does_not_retry_a_missing_asset(self) -> None:
        with tempfile.TemporaryDirectory() as directory, \
                patch("script.dex_compatibility.urllib.request.urlopen", side_effect=[self.http_error(404)]) as urlopen, \
                patch("script.dex_compatibility.time.sleep") as sleep:
            with self.assertRaisesRegex(RuntimeError, "download failed: .*404"):
                download(self.URL, Path(directory) / "checksums.txt")
        self.assertEqual(urlopen.call_count, 1)
        sleep.assert_not_called()

    def test_download_waits_out_a_secondary_rate_limit_with_jitter(self) -> None:
        responses = [self.http_error(403, {"retry-after": "7"}), io.BytesIO(b"checksums")]
        with tempfile.TemporaryDirectory() as directory, \
                patch("script.dex_compatibility.urllib.request.urlopen", side_effect=responses) as urlopen, \
                patch("script.dex_compatibility.random.uniform", return_value=0.5) as uniform, \
                patch("script.dex_compatibility.time.sleep") as sleep:
            download(self.URL, Path(directory) / "checksums.txt")
        self.assertEqual(urlopen.call_count, 2)
        uniform.assert_called_once_with(0, 2)
        self.assertEqual([call.args[0] for call in sleep.call_args_list], [7.5])

    def test_download_does_not_retry_a_permission_error(self) -> None:
        with tempfile.TemporaryDirectory() as directory, \
                patch("script.dex_compatibility.urllib.request.urlopen", side_effect=[self.http_error(403)]) as urlopen, \
                patch("script.dex_compatibility.time.sleep") as sleep:
            with self.assertRaisesRegex(RuntimeError, "download failed: .*403"):
                download(self.URL, Path(directory) / "checksums.txt")
        self.assertEqual(urlopen.call_count, 1)
        sleep.assert_not_called()

    def test_download_fails_a_rate_limit_that_resets_too_late(self) -> None:
        too_late = str(GITHUB_RATE_LIMIT_MAX_WAIT_SECONDS + 1)
        with tempfile.TemporaryDirectory() as directory, \
                patch("script.dex_compatibility.urllib.request.urlopen", side_effect=[self.http_error(429, {"retry-after": too_late})]) as urlopen, \
                patch("script.dex_compatibility.time.sleep") as sleep:
            with self.assertRaisesRegex(RuntimeError, "download failed: .*429"):
                download(self.URL, Path(directory) / "checksums.txt")
        self.assertEqual(urlopen.call_count, 1)
        sleep.assert_not_called()

    def test_rate_limit_wait_reads_the_headers_github_sends(self) -> None:
        with patch("script.dex_compatibility.time.time", return_value=1000.0):
            self.assertEqual(rate_limit_wait(self.http_error(403, {"x-ratelimit-remaining": "0", "x-ratelimit-reset": "1012"})), 12.0)
            self.assertEqual(rate_limit_wait(self.http_error(403, {"x-ratelimit-remaining": "0", "x-ratelimit-reset": "990"})), 0.0)
        self.assertEqual(rate_limit_wait(self.http_error(403, {"x-ratelimit-remaining": "0"})), 60.0)
        self.assertEqual(rate_limit_wait(self.http_error(429, {"retry-after": "3"})), 3.0)
        self.assertIsNone(rate_limit_wait(self.http_error(403, {"x-ratelimit-remaining": "12"})))
        self.assertIsNone(rate_limit_wait(self.http_error(429)))
        self.assertIsNone(rate_limit_wait(self.http_error(404, {"retry-after": "3"})))
        secondary = b'{"message": "You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}'
        self.assertEqual(rate_limit_wait(self.http_error(403, body=secondary)), 60.0)
        self.assertEqual(rate_limit_wait(self.http_error(429, body=secondary)), 60.0)
        self.assertIsNone(rate_limit_wait(self.http_error(403, body=b'{"message": "Resource not accessible by integration"}')))
        self.assertIsNone(rate_limit_wait(self.http_error(403, body=b"<html>forbidden</html>")))


class DexReleaseResolutionTest(unittest.TestCase):
    LS_REMOTE = "\n".join([
        "a1\trefs/tags/cli-v1.6.4",
        "b1\trefs/tags/cli-v1.7.0",
        "b2\trefs/tags/cli-v1.7.0^{}",
        "c1\trefs/tags/cli-v1.8.0-rc.1",
        "d1\trefs/tags/cli-v1.10.0",
        "e1\trefs/tags/cli-vnext",
    ]) + "\n"

    def release(self, tag: str, **fields: object) -> dict[str, object]:
        assets = [{"name": dexcli_archive_name(tag[4:])}, {"name": "checksums.txt"}]
        return {"tag_name": tag, "draft": False, "prerelease": False, "assets": assets, **fields}

    def resolve(self, requested: str, releases: dict[str, object]) -> tuple[dict[str, object], list[str]]:
        listed = subprocess.CompletedProcess([], 0, stdout=self.LS_REMOTE, stderr="")
        requested_urls: list[str] = []

        def github_json(url: str) -> object:
            requested_urls.append(url)
            tag = url.rsplit("/", 1)[1]
            if tag not in releases:
                raise urllib.error.HTTPError(url, 404, "Not Found", {}, None)
            return releases[tag]

        with patch("script.dex_compatibility.run", return_value=listed), \
                patch("script.dex_compatibility.github_json", side_effect=github_json):
            return resolve_dex_release(requested), requested_urls

    def test_stable_cli_tags_prefer_the_peeled_commit_and_skip_other_tags(self) -> None:
        listed = subprocess.CompletedProcess([], 0, stdout=self.LS_REMOTE, stderr="")
        with patch("script.dex_compatibility.run", return_value=listed):
            self.assertEqual(stable_cli_tags("superdurable/dex"), {"cli-v1.6.4": "a1", "cli-v1.7.0": "b2", "cli-v1.10.0": "d1"})

    def test_the_highest_stable_release_costs_one_api_request(self) -> None:
        release, urls = self.resolve("latest", {"cli-v1.10.0": self.release("cli-v1.10.0"), "cli-v1.7.0": self.release("cli-v1.7.0")})
        self.assertEqual((release["tag_name"], release["resolved_commit"]), ("cli-v1.10.0", "d1"))
        self.assertEqual(urls, ["https://api.github.com/repos/superdurable/dex/releases/tags/cli-v1.10.0"])

    def test_tags_without_a_finished_stable_release_are_skipped(self) -> None:
        for newest in ({}, {"cli-v1.10.0": self.release("cli-v1.10.0", prerelease=True)},
                       {"cli-v1.10.0": self.release("cli-v1.10.0", assets=[{"name": "checksums.txt"}])}):
            release, urls = self.resolve("latest", {**newest, "cli-v1.7.0": self.release("cli-v1.7.0")})
            self.assertEqual((release["tag_name"], release["resolved_commit"]), ("cli-v1.7.0", "b2"))
            self.assertEqual(len(urls), 2)

    def test_an_exact_version_resolves_only_that_tag(self) -> None:
        release, urls = self.resolve("v1.6.4", {"cli-v1.6.4": self.release("cli-v1.6.4"), "cli-v1.10.0": self.release("cli-v1.10.0")})
        self.assertEqual(release["tag_name"], "cli-v1.6.4")
        self.assertEqual(urls, ["https://api.github.com/repos/superdurable/dex/releases/tags/cli-v1.6.4"])
        with self.assertRaisesRegex(RuntimeError, "no stable Dex CLI release found for v1.8.0-rc.1"):
            self.resolve("v1.8.0-rc.1", {})


if __name__ == "__main__":
    unittest.main()
