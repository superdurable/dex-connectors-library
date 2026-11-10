# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

import hashlib
import io
import json
import os
import subprocess
import tarfile
import tempfile
import unittest
from pathlib import Path

from script.release import connectors_mcp_release as release

ROOT = Path(__file__).resolve().parents[2]
TREE = "1" * 40
OTHER_TREE = "2" * 40
COMMIT = "3" * 40


def connector(connector_id: str, version: str, *, package_name: str | None = None, tools: list[str] | None = None) -> dict:
    return {"connectorId": connector_id, "displayName": connector_id.title(), "directory": f"connectors/{connector_id}",
            "module": f"github.com/superdurable/dex-connectors-library/connectors/{connector_id}/go", "version": version,
            "tag": f"connectors/{connector_id}/go/{version}", "packageName": package_name or connector_id.replace("-", ""),
            "tools": tools or [f"{connector_id}_list_items"]}


def inventory(*connectors: dict, series: str = "v0.1", tree: str = TREE, withdrawn: list[str] | None = None) -> dict:
    return {"series": series, "withdrawnConnectors": withdrawn or [], "sourceCommit": COMMIT,
            "serverSourceTree": tree, "connectors": list(connectors)}


def lock(version: str, *connectors: dict, tree: str = TREE, launch_contract: int = 1) -> dict:
    return {
        "schemaVersion": release.LOCK_SCHEMA_VERSION, "version": version, "tag": release.TAG_PREFIX + version,
        "launchContract": launch_contract, "sourceCommit": "4" * 40, "mainCommit": COMMIT, "serverSourceTree": tree,
        "goVersion": "go1.25.4", "sdkgoVersion": "v0.35.0", "dexSdkGoVersion": "v1.40.2",
        "connectors": [{key: value for key, value in entry.items() if key not in ("directory", "tag")} for entry in connectors],
        "assets": {release.platform_key(goos, goarch): {"name": release.archive_name(version, goos, goarch),
                                                        "sha256": hashlib.sha256(f"{version}-{goos}-{goarch}".encode()).hexdigest()}
                   for goos, goarch in release.PLATFORMS},
    }


def plan_with(previous: dict | None, *connectors: dict, **inventory_options) -> release.ReleasePlan:
    return release.plan_release(inventory(*connectors, **inventory_options), previous,
                                previous["tag"] if previous else None)


class NextServerVersionTests(unittest.TestCase):
    def test_takes_the_next_patch_in_the_series(self) -> None:
        self.assertEqual("0.1.0", release.next_server_version("v0.1", None))
        self.assertEqual("0.1.4", release.next_server_version("v0.1", "0.1.3"))
        self.assertEqual("0.2.0", release.next_server_version("v0.2", "0.1.3"), "a series bump starts at its .0")
        self.assertEqual("1.0.0", release.next_server_version("v1.0", "0.9.12"))

    def test_rejects_a_series_behind_the_latest_release_or_malformed(self) -> None:
        with self.assertRaisesRegex(release.ConnectorsMCPReleaseError, "behind the latest release"):
            release.next_server_version("v0.1", "0.2.0")
        with self.assertRaisesRegex(release.ConnectorsMCPReleaseError, "vMAJOR.MINOR"):
            release.next_server_version("v0.1.0", None)


class ReleaseStateTests(unittest.TestCase):
    def test_finds_the_latest_complete_release_and_a_newer_incomplete_tag(self) -> None:
        inventory_assets = {
            "connectorsmcp/v0.1.0": [release.RELEASE_COMPLETE_ASSET],
            "connectorsmcp/v0.1.1": [release.RELEASE_COMPLETE_ASSET, release.RELEASE_LOCK_ASSET],
            "connectorsmcp/v0.1.2": [release.RELEASE_LOCK_ASSET],
            "connectors/acme/go/v9.9.9": ["connector-release.complete"],
        }
        state = release.release_state(inventory_assets, ["connectorsmcp/v0.1.0", "connectorsmcp/v0.1.1", "connectorsmcp/v0.1.2",
                                                         "connectorsmcp/v0.1.3", "connectorsmcp/latest"])
        self.assertEqual(release.ReleaseState("connectorsmcp/v0.1.1", "connectorsmcp/v0.1.3"), state)

    def test_ignores_an_abandoned_tag_below_the_latest_complete_release(self) -> None:
        state = release.release_state({"connectorsmcp/v0.1.10": [release.RELEASE_COMPLETE_ASSET]},
                                      ["connectorsmcp/v0.1.9", "connectorsmcp/v0.1.10"])
        self.assertEqual(release.ReleaseState("connectorsmcp/v0.1.10", None), state, "versions compare numerically")
        self.assertEqual(release.ReleaseState(None, None), release.release_state({}, []))

    def test_refuses_an_unavailable_inventory(self) -> None:
        with self.assertRaisesRegex(release.ConnectorsMCPReleaseError, "unavailable"):
            release.release_state({"_inventory_unavailable": True}, [])


class PlanReleaseTests(unittest.TestCase):
    def test_first_release_starts_the_series(self) -> None:
        plan = plan_with(None, connector("slack", "v1.4.0"), connector("hacker-news-daily", "v0.5.0"))
        self.assertTrue(plan.is_release_required)
        self.assertEqual(("0.1.0", "connectorsmcp/v0.1.0"), (plan.version, plan.tag))
        self.assertEqual(["hacker-news-daily", "slack"], [entry.connector_id for entry in plan.desired])

    def test_unchanged_connector_set_and_server_source_skip_the_release(self) -> None:
        previous = lock("0.1.3", connector("slack", "v1.4.0"))
        plan = plan_with(previous, connector("slack", "v1.4.0"))
        self.assertFalse(plan.is_release_required)
        self.assertIn("match connectorsmcp/v0.1.3", plan.reason)

    def test_a_connector_or_server_change_takes_the_next_patch(self) -> None:
        previous = lock("0.1.3", connector("slack", "v1.4.0"))
        for name, plan in (
            ("connector upgrade", plan_with(previous, connector("slack", "v1.5.0"))),
            ("new connector", plan_with(previous, connector("slack", "v1.4.0"), connector("reddit", "v0.3.0"))),
            ("server source", plan_with(previous, connector("slack", "v1.4.0"), tree=OTHER_TREE)),
        ):
            with self.subTest(name):
                self.assertTrue(plan.is_release_required)
                self.assertEqual("connectorsmcp/v0.1.4", plan.tag)
        self.assertEqual("connectorsmcp/v0.2.0", plan_with(previous, connector("slack", "v1.4.0"), series="v0.2", tree=OTHER_TREE).tag)

    def test_never_silently_drops_a_previously_included_connector(self) -> None:
        previous = lock("0.1.3", connector("slack", "v1.4.0"), connector("reddit", "v0.3.0"))
        with self.assertRaisesRegex(release.ConnectorsMCPReleaseError, "reddit v0.3.0 is in connectorsmcp/v0.1.3.*withdrawnConnectors"):
            plan_with(previous, connector("slack", "v1.5.0"))

    def test_withdrawn_connectors_leave_deliberately(self) -> None:
        previous = lock("0.1.3", connector("slack", "v1.4.0"), connector("reddit", "v0.3.0"))
        plan = plan_with(previous, connector("slack", "v1.4.0"), connector("reddit", "v0.4.0"), withdrawn=["reddit"])
        self.assertTrue(plan.is_release_required)
        self.assertEqual(["slack"], [entry.connector_id for entry in plan.desired])

    def test_never_downgrades_a_previously_included_connector(self) -> None:
        previous = lock("0.1.3", connector("slack", "v1.4.0"))
        with self.assertRaisesRegex(release.ConnectorsMCPReleaseError, "older v1.3.9; a release never downgrades"):
            plan_with(previous, connector("slack", "v1.3.9"))

    def test_withdrawing_the_last_connector_releases_an_empty_server(self) -> None:
        previous = lock("0.1.3", connector("slack", "v1.4.0"))
        plan = plan_with(previous, connector("slack", "v1.4.0"), withdrawn=["slack"])
        self.assertTrue(plan.is_release_required, "the index must stop advertising a withdrawn connector")
        self.assertEqual(("connectorsmcp/v0.1.4", []), (plan.tag, plan.desired))
        evaluated = []
        result = release.select_release_set(plan, lambda connectors: evaluated.append(connectors) or True)
        self.assertEqual(([[]], []), (evaluated, result.connectors))
        self.assertTrue(release.is_release_required_after_hold(plan, result))
        empty_previous = lock("0.1.4")
        self.assertFalse(plan_with(empty_previous, withdrawn=["slack"], tree=OTHER_TREE).is_release_required)

    def test_no_release_without_a_connector(self) -> None:
        plan = plan_with(None)
        self.assertFalse(plan.is_release_required)
        self.assertEqual("no connector release offers agent tools", plan.reason)

    def test_rejects_duplicate_inventory_entries_and_an_invalid_lock(self) -> None:
        with self.assertRaisesRegex(release.ConnectorsMCPReleaseError, "listed twice"):
            plan_with(None, connector("slack", "v1.4.0"), connector("slack", "v1.5.0"))
        broken = lock("0.1.3", connector("slack", "v1.4.0"))
        broken["launchContract"] = "1"
        with self.assertRaisesRegex(release.ConnectorsMCPReleaseError, "launchContract must be a positive integer"):
            plan_with(broken, connector("slack", "v1.4.0"))

    def test_plan_round_trips_through_json(self) -> None:
        plan = plan_with(lock("0.1.3", connector("slack", "v1.4.0")), connector("slack", "v1.5.0"))
        self.assertEqual(release.plan_to_json(plan), release.plan_to_json(release.plan_from_json(
            json.loads(json.dumps(release.plan_to_json(plan))))))


class HoldRuleTests(unittest.TestCase):
    def evaluator(self, failing: set[tuple[str, str]], *, fail_previous_set: bool = False):
        calls: list[dict[str, str]] = []

        def evaluate(connectors: list[release.LinkedConnector]) -> bool:
            versions = release.connector_versions(connectors)
            calls.append(versions)
            if fail_previous_set and versions == {"slack": "v1.4.0"}:
                return False
            return not any((connector_id, version) in failing for connector_id, version in versions.items())

        return evaluate, calls

    def test_the_full_set_ships_when_it_passes(self) -> None:
        plan = plan_with(lock("0.1.3", connector("slack", "v1.4.0")), connector("slack", "v1.5.0"), connector("reddit", "v0.3.0"))
        evaluate, calls = self.evaluator(set())
        result = release.select_release_set(plan, evaluate)
        self.assertEqual({"reddit": "v0.3.0", "slack": "v1.5.0"}, release.connector_versions(result.connectors))
        self.assertEqual(([], []), (result.held, result.excluded))
        self.assertEqual(1, len(calls))

    def test_a_failing_upgrade_is_held_and_a_failing_new_connector_is_excluded(self) -> None:
        previous = lock("0.1.3", connector("slack", "v1.4.0"), connector("gmail", "v0.2.0"))
        plan = plan_with(previous, connector("slack", "v1.5.0"), connector("gmail", "v0.3.0"),
                         connector("reddit", "v0.3.0"), connector("spotify", "v0.1.0"))
        evaluate, calls = self.evaluator({("slack", "v1.5.0"), ("reddit", "v0.3.0")})
        result = release.select_release_set(plan, evaluate)
        self.assertEqual({"gmail": "v0.3.0", "slack": "v1.4.0", "spotify": "v0.1.0"}, release.connector_versions(result.connectors))
        self.assertEqual([release.HeldConnector("slack", "v1.4.0", "v1.5.0")], result.held)
        self.assertEqual(["reddit"], [entry.connector_id for entry in result.excluded])
        self.assertEqual({"gmail": "v0.2.0", "slack": "v1.4.0"}, calls[1], "the previous set is checked first")
        self.assertEqual("Slack", next(entry for entry in result.connectors if entry.connector_id == "slack").display_name)
        self.assertTrue(release.is_release_required_after_hold(plan, result))

    def test_never_drops_a_previously_included_connector_when_the_previous_set_fails(self) -> None:
        plan = plan_with(lock("0.1.3", connector("slack", "v1.4.0")), connector("slack", "v1.5.0"), tree=OTHER_TREE)
        evaluate, _ = self.evaluator({("slack", "v1.5.0")}, fail_previous_set=True)
        with self.assertRaisesRegex(release.ConnectorsMCPReleaseError, "never drops or downgrades"):
            release.select_release_set(plan, evaluate)

    def test_every_change_held_with_unchanged_source_needs_no_release(self) -> None:
        plan = plan_with(lock("0.1.3", connector("slack", "v1.4.0")), connector("slack", "v1.5.0"))
        evaluate, _ = self.evaluator({("slack", "v1.5.0")})
        result = release.select_release_set(plan, evaluate)
        self.assertEqual({"slack": "v1.4.0"}, release.connector_versions(result.connectors))
        self.assertFalse(release.is_release_required_after_hold(plan, result))
        plan.server_source_tree = OTHER_TREE
        self.assertTrue(release.is_release_required_after_hold(plan, result), "a server change still ships")

    def test_first_release_excludes_failing_connectors_and_ships_the_rest(self) -> None:
        plan = plan_with(None, connector("slack", "v1.4.0"), connector("reddit", "v0.3.0"))
        evaluate, calls = self.evaluator({("reddit", "v0.3.0")})
        result = release.select_release_set(plan, evaluate)
        self.assertEqual({"slack": "v1.4.0"}, release.connector_versions(result.connectors))
        self.assertEqual({}, calls[1], "the server builds without connectors before any is added")


class RegistryTests(unittest.TestCase):
    def test_empty_registry_is_the_file_on_main(self) -> None:
        self.assertEqual((ROOT / "connectorsmcp" / release.REGISTRY_PATH).read_text(encoding="utf-8"),
                         release.generate_registry_source([], format_source=release.gofmt_source))

    def test_generated_registry_is_gofmt_clean_and_reads_back(self) -> None:
        connectors = [release.LinkedConnector.from_json(entry, "test") for entry in (
            connector("slack", "v1.4.0", package_name="slack"),
            connector("hacker-news-daily", "v0.5.0", package_name="hackernews"),
            {**connector("slack-enterprise", "v0.2.0", package_name="slack"), "displayName": 'Slack "Grid" é'},
            connector("sdk-shadow", "v0.1.0", package_name="sdkgo"),
        )]
        source = release.generate_registry_source(connectors, format_source=release.gofmt_source)
        self.assertEqual(source, release.gofmt_source(source))
        self.assertIn('\tslack "github.com/superdurable/dex-connectors-library/connectors/slack/go"\n', source)
        self.assertIn('\tslack2 "github.com/superdurable/dex-connectors-library/connectors/slack-enterprise/go"\n', source)
        self.assertIn('\tsdkgo2 "github.com/superdurable/dex-connectors-library/connectors/sdk-shadow/go"\n', source)
        self.assertIn("connection, err := slack2.NewProjectConnection(project, connectionName)", source)
        self.assertTrue(source.startswith("// Code generated by the Connectors MCP release workflow; DO NOT EDIT.\n"))
        self.assertEqual([{"connectorId": entry.connector_id, "displayName": entry.display_name, "module": entry.module,
                           "version": entry.version} for entry in release.sorted_connectors(connectors)],
                         release.read_registry_entries(source))


class VersionReportTests(unittest.TestCase):
    def test_requires_the_exact_linked_connectors_and_a_launch_contract(self) -> None:
        linked = [{"connectorId": "slack", "module": "m/slack", "version": "v1.4.0", "tools": ["slack_a"], "displayName": "Slack"}]
        report = {"version": "0.1.0", "launchContract": 1,
                  "connectors": [{"connectorId": "slack", "module": "m/slack", "version": "v1.4.0", "tools": ["slack_a"]}]}
        self.assertEqual(1, release.verify_version_report(report, linked, "0.1.0"))
        renamed = {**report, "connectors": [{**report["connectors"][0], "tools": ["slack_renamed"]}]}
        self.assertEqual(1, release.verify_version_report(renamed, linked, "0.1.0"), "released code owns the tool names")
        for name, changed, message in (
            ("tool count", {**report, "connectors": [{**report["connectors"][0], "tools": ["slack_a", "slack_b"]}]}, "expected 1"),
            ("tool prefix", {**report, "connectors": [{**report["connectors"][0], "tools": ["gmail_a"]}]}, "named slack_"),
            ("module version", {**report, "connectors": [{**report["connectors"][0], "version": "v1.5.0"}]}, "the binary links"),
            ("extra connector", {**report, "connectors": report["connectors"] * 2}, "the binary links"),
            ("version", {**report, "version": "devel"}, "reports version"),
            ("launch contract", {**report, "launchContract": True}, "launch contract"),
        ):
            with self.subTest(name), self.assertRaisesRegex(release.ConnectorsMCPReleaseError, message):
                release.verify_version_report(changed, linked, "0.1.0")


class FakeGo:
    """Stands in for the go toolchain and the built binary in ServerBuild."""

    def __init__(self, failing_module: str | None = None) -> None:
        self.failing_module = failing_module
        self.commands: list[list[str]] = []

    def __call__(self, command, cwd=None, capture_output=True, text=True, env=None):
        self.commands.append(command)
        module_directory = Path(cwd) if cwd else None
        if command[:3] == ["go", "mod", "edit"]:
            go_mod = module_directory / "go.mod"
            go_mod.write_text(go_mod.read_text() + "".join(f"require {argument[9:].replace('@', ' ')}\n" for argument in command[3:]))
        elif command[:3] == ["go", "mod", "tidy"]:
            requirements = [line for line in (module_directory / "go.mod").read_text().splitlines() if line.startswith("require ")]
            (module_directory / "go.sum").write_text("".join(f"{line[8:]} h1:x\n" for line in requirements))
            if self.failing_module and any(self.failing_module in line for line in requirements):
                return subprocess.CompletedProcess(command, 1, "", f"{self.failing_module}: undefined: sdkgo.Removed")
        elif command[:3] == ["go", "list", "-m"]:
            requirements = dict(line[8:].split(" ") for line in (module_directory / "go.mod").read_text().splitlines()
                                if line.startswith("require "))
            requirements |= {release.SDK_MODULE_PATH: "v0.35.0", release.DEX_SDK_MODULE_PATH: "v1.40.2"}
            return subprocess.CompletedProcess(command, 0, "".join(f"{path} {requirements[path]}\n" for path in command[5:]), "")
        elif command[:3] == ["go", "list", "-f"]:
            return subprocess.CompletedProcess(command, 0, command[4].split("/")[-2].replace("-", "") + "\n", "")
        elif command[:3] == ["go", "env", "GOVERSION"]:
            return subprocess.CompletedProcess(command, 0, "go1.25.4\n", "")
        elif command[:2] == ["go", "build"]:
            output = Path(command[command.index("-o") + 1])
            output.parent.mkdir(parents=True, exist_ok=True)
            flags = command[command.index("-ldflags") + 1]
            version = flags.split("-X main.version=")[1] if "-X main.version=" in flags else "devel"
            platform = f"{(env or {}).get('GOOS', 'host')}/{(env or {}).get('GOARCH', 'host')}"
            output.write_text(json.dumps({"version": version, "platform": platform,
                                          "registry": (module_directory / release.REGISTRY_PATH).read_text()}))
        elif command[1:] == ["version", "--output", "json"]:
            binary = json.loads(Path(command[0]).read_text())
            entries = release.read_registry_entries(binary["registry"])
            return subprocess.CompletedProcess(command, 0, json.dumps({"version": binary["version"], "launchContract": 1, "connectors": [
                {"connectorId": entry["connectorId"], "module": entry["module"], "version": entry["version"],
                 "tools": [entry["connectorId"] + "_list_items"]} for entry in entries]}), "")
        return subprocess.CompletedProcess(command, 0, "", "")


class ReleaseSetEvaluatorTests(unittest.TestCase):
    def test_restores_main_files_between_sets_and_applies_the_passing_set(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            module = Path(directory) / "connectorsmcp"
            (module / release.REGISTRY_PATH).parent.mkdir(parents=True)
            (module / "go.mod").write_text("module github.com/superdurable/dex-connectors-library/connectorsmcp\n")
            (module / "go.sum").write_text("")
            main_registry = release.generate_registry_source([], format_source=release.gofmt_source)
            (module / release.REGISTRY_PATH).write_text(main_registry)
            fake_go = FakeGo(failing_module="connectors/slack/go")
            evaluator = release.ReleaseSetEvaluator(release.ServerBuild(module, Path(directory) / "work", run=fake_go),
                                                    format_source=release.gofmt_source, log=lambda _message: None)
            plan = plan_with(None, connector("slack", "v1.4.0"), connector("reddit", "v0.3.0"))

            result = release.select_release_set(plan, evaluator)
            self.assertEqual({"reddit": "v0.3.0"}, release.connector_versions(result.connectors))
            self.assertIn("slack", (module / "go.mod").read_text(), "the last evaluation tried slack")
            evaluator.apply(result.connectors)

            go_mod = (module / "go.mod").read_text()
            self.assertIn("require github.com/superdurable/dex-connectors-library/connectors/reddit/go v0.3.0", go_mod)
            self.assertNotIn("slack", go_mod + (module / "go.sum").read_text())
            self.assertEqual(["reddit"], [entry["connectorId"] for entry in release.read_registry_entries(
                (module / release.REGISTRY_PATH).read_text())])
            self.assertIn(["go", "test", "-race", "-count=1", "./..."], fake_go.commands)
            self.assertIn(["go", "vet", "./..."], fake_go.commands)


def run_git(repository: Path, *arguments: str) -> str:
    return subprocess.run(["git", *arguments], cwd=repository, check=True, capture_output=True, text=True, env={
        "GIT_AUTHOR_NAME": "Fixture", "GIT_AUTHOR_EMAIL": "fixture@example.com", "GIT_COMMITTER_NAME": "Fixture",
        "GIT_COMMITTER_EMAIL": "fixture@example.com", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1",
        "PATH": os.environ["PATH"]}).stdout.strip()


class PackageReleaseTests(unittest.TestCase):
    def test_packages_the_tagged_release_commit_with_its_lock_and_checksums(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            repository = Path(directory)
            module = repository / "connectorsmcp"
            (module / release.REGISTRY_PATH).parent.mkdir(parents=True)
            (repository / "LICENSE").write_text("MIT License\n")
            (module / "README.md").write_text("# Dex Connectors MCP server\n")
            (module / "go.mod").write_text("module github.com/superdurable/dex-connectors-library/connectorsmcp\n")
            (module / release.REGISTRY_PATH).write_text(release.generate_registry_source([], format_source=release.gofmt_source))
            run_git(repository, "init", "-q", "-b", "main")
            run_git(repository, "add", "-A")
            run_git(repository, "commit", "-q", "-m", "main")
            main_commit = run_git(repository, "rev-parse", "HEAD")
            server_tree = run_git(repository, "rev-parse", "HEAD:connectorsmcp")
            slack = release.LinkedConnector.from_json(connector("slack", "v1.4.0", tools=["slack_list_items"]), "test")
            (module / release.REGISTRY_PATH).write_text(release.generate_registry_source([slack], format_source=release.gofmt_source))
            (module / "go.mod").write_text((module / "go.mod").read_text() + f"require {slack.module} v1.4.0\n")
            run_git(repository, "commit", "-q", "-am", "connectorsmcp: release v0.1.0")
            run_git(repository, "tag", "connectorsmcp/v0.1.0")
            output = repository / "dist"

            lock = release.package_release(
                repository=repository, module_directory=module, version="0.1.0", tag="connectorsmcp/v0.1.0",
                output_directory=output, work_directory=repository / "work",
                build=release.ServerBuild(module, repository / "work", run=FakeGo()),
                held=[release.HeldConnector("reddit", "v0.3.0", "v0.4.0")])

            self.assertEqual(json.loads((output / release.RELEASE_LOCK_ASSET).read_text()), lock)
            self.assertEqual((main_commit, server_tree, run_git(repository, "rev-parse", "HEAD")),
                             (lock["mainCommit"], lock["serverSourceTree"], lock["sourceCommit"]))
            self.assertEqual(("go1.25.4", "v0.35.0", "v1.40.2"), (lock["goVersion"], lock["sdkgoVersion"], lock["dexSdkGoVersion"]))
            self.assertEqual([{**slack.to_json(), "packageName": "slack"}], lock["connectors"])
            checksums = (output / release.CHECKSUMS_ASSET).read_text().splitlines()
            self.assertEqual(5, len(checksums))
            for line in checksums:
                digest, name = line.split("  ")
                self.assertEqual(hashlib.sha256((output / name).read_bytes()).hexdigest(), digest)
            for platform, asset in lock["assets"].items():
                with tarfile.open(output / asset["name"]) as archive:
                    binary = json.loads(archive.extractfile(release.BINARY_NAME).read())
                self.assertEqual(("0.1.0", platform.replace("-", "/")), (binary["version"], binary["platform"]))
            notes = (repository / "work" / "release-notes.md").read_text()
            self.assertIn("| slack | `github.com/superdurable/dex-connectors-library/connectors/slack/go@v1.4.0` | `slack_list_items` |", notes)
            self.assertIn("- reddit: v0.3.0 (not v0.4.0)", notes)

    def test_packages_an_empty_server_after_the_last_withdrawal(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            repository = Path(directory)
            module = repository / "connectorsmcp"
            (module / release.REGISTRY_PATH).parent.mkdir(parents=True)
            (repository / "LICENSE").write_text("MIT License\n")
            (module / "README.md").write_text("# Dex Connectors MCP server\n")
            (module / "go.mod").write_text("module github.com/superdurable/dex-connectors-library/connectorsmcp\n")
            (module / release.REGISTRY_PATH).write_text(release.generate_registry_source([], format_source=release.gofmt_source))
            run_git(repository, "init", "-q", "-b", "main")
            run_git(repository, "add", "-A")
            run_git(repository, "commit", "-q", "-m", "main")
            # The registry equals main's, so the release commit is empty.
            run_git(repository, "commit", "-q", "--allow-empty", "-m", "connectorsmcp: release v0.1.4")
            run_git(repository, "tag", "connectorsmcp/v0.1.4")

            lock_data = release.package_release(
                repository=repository, module_directory=module, version="0.1.4", tag="connectorsmcp/v0.1.4",
                output_directory=repository / "dist", work_directory=repository / "work",
                build=release.ServerBuild(module, repository / "work", run=FakeGo()))

            self.assertEqual([], lock_data["connectors"])
            self.assertIn("links no connector release", (repository / "work" / "release-notes.md").read_text())

    def test_refuses_a_tag_that_does_not_name_the_checked_out_commit(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            repository = Path(directory)
            (repository / "file").write_text("x")
            run_git(repository, "init", "-q", "-b", "main")
            run_git(repository, "add", "-A")
            run_git(repository, "commit", "-q", "-m", "main")
            run_git(repository, "tag", "connectorsmcp/v0.1.0")
            run_git(repository, "commit", "-q", "--allow-empty", "-m", "later")
            with self.assertRaisesRegex(release.ConnectorsMCPReleaseError, "does not name the checked-out release commit"):
                release.package_release(repository=repository, module_directory=repository, version="0.1.0",
                                        tag="connectorsmcp/v0.1.0", output_directory=repository / "dist",
                                        work_directory=repository / "work",
                                        build=release.ServerBuild(repository, repository / "work", run=FakeGo()))


class ArchiveTests(unittest.TestCase):
    def test_archive_holds_the_binary_license_and_readme_at_the_top_level(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / "dex-connectors-mcp"
            binary.write_bytes(b"\x7fELF binary")
            archives = []
            for name in ("first.tar.gz", "second.tar.gz"):
                release.write_release_archive(root / name, binary, ROOT / "LICENSE", ROOT / "connectorsmcp" / "README.md", 1_700_000_000)
                archives.append((root / name).read_bytes())
            self.assertEqual(archives[0], archives[1], "archives are reproducible")
            with tarfile.open(fileobj=io.BytesIO(archives[0]), mode="r:gz") as archive:
                members = archive.getmembers()
                self.assertEqual(["dex-connectors-mcp", "LICENSE", "README.md"], [member.name for member in members])
                self.assertTrue(all(member.isreg() for member in members), "no symlinks or directories")
                self.assertEqual([0o755, 0o644, 0o644], [member.mode for member in members])
                self.assertEqual(b"\x7fELF binary", archive.extractfile("dex-connectors-mcp").read())

    def test_archive_names_follow_the_dexcli_contract(self) -> None:
        self.assertEqual("dex-connectors-mcp_0.1.3_darwin_arm64.tar.gz", release.archive_name("0.1.3", "darwin", "arm64"))
        self.assertEqual({"darwin-arm64", "darwin-amd64", "linux-amd64", "linux-arm64"},
                         {release.platform_key(*platform) for platform in release.PLATFORMS})


def github_release(version: str, release_lock: dict | None, *, complete: bool = True, prerelease: bool = False,
                   digests: dict[str, str] | None = None) -> dict:
    tag = release.TAG_PREFIX + version
    base = f"https://github.com/superdurable/dex-connectors-library/releases/download/{tag}"
    assets = [{"name": release.archive_name(version, goos, goarch),
               "browser_download_url": f"{base}/{release.archive_name(version, goos, goarch)}",
               **({"digest": digests[release.archive_name(version, goos, goarch)]} if digests and
                  release.archive_name(version, goos, goarch) in digests else {})}
              for goos, goarch in release.PLATFORMS]
    assets.append({"name": release.CHECKSUMS_ASSET, "browser_download_url": f"{base}/{release.CHECKSUMS_ASSET}"})
    if release_lock is not None:
        assets.append({"name": release.RELEASE_LOCK_ASSET, "browser_download_url": f"{base}/{release.RELEASE_LOCK_ASSET}"})
    if complete:
        assets.append({"name": release.RELEASE_COMPLETE_ASSET, "browser_download_url": f"{base}/{release.RELEASE_COMPLETE_ASSET}"})
    return {"tag_name": tag, "draft": False, "prerelease": prerelease, "published_at": f"2026-11-{10 + int(version[-1]):02d}T10:00:00Z",
            "assets": assets}


class ReleaseIndexTests(unittest.TestCase):
    def build(self, releases: list[dict], locks: dict[str, object], archive_bytes: dict[str, bytes] | None = None,
              fetched: list[str] | None = None) -> tuple[dict, list[str]]:
        warnings: list[str] = []
        archive_bytes = archive_bytes or {}
        fetched = fetched if fetched is not None else []

        def fetch(url: str) -> bytes:
            fetched.append(url)
            name = url.rsplit("/", 1)[-1]
            if name.endswith(".tar.gz"):
                _, version, goos, goarch = name.removesuffix(".tar.gz").split("_")
                return archive_bytes.get(name, f"{version}-{goos}-{goarch}".encode())
            value = locks[url.rsplit("/", 2)[-2]]
            return value if isinstance(value, bytes) else json.dumps(value).encode()

        return release.build_release_index(releases, fetch, warnings.append), warnings

    def test_index_lists_complete_releases_newest_first_with_archive_digests(self) -> None:
        slack = connector("slack", "v1.4.0", tools=["slack_list_thread_messages", "slack_post_thread_reply"])
        first, second = lock("0.1.1", slack), lock("0.1.2", slack, connector("gmail", "v0.3.0"))
        releases = [github_release("0.1.1", first), github_release("0.1.2", second),
                    github_release("0.1.3", lock("0.1.3", slack), complete=False),
                    github_release("0.1.4", lock("0.1.4", slack), prerelease=True),
                    {"tag_name": "connectors/slack/go/v1.4.0", "assets": [{"name": release.RELEASE_COMPLETE_ASSET}]}]
        index, warnings = self.build(releases, {"v0.1.1": first, "v0.1.2": second})

        self.assertEqual([], warnings)
        self.assertEqual({"schemaVersion", "releases"}, set(index))
        self.assertEqual("dex-connectors-mcp/release-index/v1", index["schemaVersion"])
        self.assertEqual(["0.1.2", "0.1.1"], [entry["version"] for entry in index["releases"]])
        newest = index["releases"][0]
        self.assertEqual({"version", "launchContract", "tag", "publishedAt", "assets", "connectors"}, set(newest))
        self.assertIs(type(newest["launchContract"]), int)
        self.assertEqual(1, newest["launchContract"])
        self.assertEqual("connectorsmcp/v0.1.2", newest["tag"])
        self.assertEqual("2026-11-12T10:00:00Z", newest["publishedAt"])
        self.assertEqual({"darwin-arm64", "darwin-amd64", "linux-amd64", "linux-arm64"}, set(newest["assets"]))
        for platform, asset in newest["assets"].items():
            self.assertEqual({"url", "sha256"}, set(asset), "dexcli verifies the archive digest from the index alone")
            self.assertEqual(second["assets"][platform]["sha256"], asset["sha256"])
            self.assertTrue(asset["url"].endswith(second["assets"][platform]["name"]))
        self.assertEqual([{"connectorId": "slack", "module": slack["module"], "version": "v1.4.0",
                           "tools": ["slack_list_thread_messages", "slack_post_thread_reply"]},
                          {"connectorId": "gmail", "module": connector("gmail", "v0.3.0")["module"], "version": "v0.3.0",
                           "tools": ["gmail_list_items"]}], newest["connectors"])
        self.assertNotIn("checksumsUrl", json.dumps(index))

    def test_archives_without_a_github_digest_are_downloaded_and_hashed(self) -> None:
        good = lock("0.1.1", connector("slack", "v1.4.0"))
        replaced = lock("0.1.2", connector("slack", "v1.4.0"))
        newest = lock("0.1.3", connector("slack", "v1.4.0"))
        all_digests = {asset["name"]: "sha256:" + asset["sha256"] for asset in newest["assets"].values()}
        releases = [github_release("0.1.1", good), github_release("0.1.2", replaced), github_release("0.1.3", newest, digests=all_digests)]
        fetched: list[str] = []
        index, warnings = self.build(releases, {"v0.1.1": good, "v0.1.2": replaced, "v0.1.3": newest},
                                     archive_bytes={release.archive_name("0.1.2", "darwin", "arm64"): b"replaced upload"}, fetched=fetched)

        self.assertEqual(["0.1.3", "0.1.1"], [entry["version"] for entry in index["releases"]])
        self.assertEqual(1, len(warnings))
        self.assertIn("connectorsmcp/v0.1.2", warnings[0])
        self.assertIn("not the locked", warnings[0])
        self.assertFalse(any("v0.1.3/" in url and url.endswith(".tar.gz") for url in fetched),
                         "a GitHub digest avoids downloading the archive")
        self.assertEqual(4, sum("v0.1.1/" in url and url.endswith(".tar.gz") for url in fetched))

    def test_the_newest_release_must_verify_or_the_index_fails(self) -> None:
        older = lock("0.1.1", connector("slack", "v1.4.0"))
        newest = lock("0.1.2", connector("slack", "v1.4.0"))
        releases = [github_release("0.1.1", older), github_release("0.1.2", newest)]
        corrupted = {release.archive_name("0.1.2", "linux", "amd64"): b"corrupted"}
        with self.assertRaisesRegex(release.ConnectorsMCPReleaseError, "newest Connectors MCP release connectorsmcp/v0.1.2"):
            self.build(releases, {"v0.1.1": older, "v0.1.2": newest}, archive_bytes=corrupted)

    def test_a_broken_release_is_left_out_with_a_warning(self) -> None:
        good = lock("0.1.1", connector("slack", "v1.4.0"))
        newest = lock("0.1.6", connector("slack", "v1.4.0"))
        wrong_identity = lock("0.1.2", connector("slack", "v1.4.0"))
        wrong_identity["tag"] = "connectorsmcp/v0.1.9"
        digest_mismatch = lock("0.1.3", connector("slack", "v1.4.0"))
        releases = [github_release("0.1.1", good, digests={release.archive_name("0.1.1", "linux", "amd64"):
                                                           "sha256:" + good["assets"]["linux-amd64"]["sha256"]}),
                    github_release("0.1.2", wrong_identity),
                    github_release("0.1.3", digest_mismatch, digests={release.archive_name("0.1.3", "linux", "amd64"): "sha256:" + "0" * 64}),
                    github_release("0.1.4", None),
                    github_release("0.1.5", lock("0.1.5", connector("slack", "v1.4.0"))),
                    github_release("0.1.6", newest)]
        index, warnings = self.build(releases, {"v0.1.1": good, "v0.1.2": wrong_identity, "v0.1.3": digest_mismatch,
                                                "v0.1.5": b"{", "v0.1.6": newest})

        self.assertEqual(["0.1.6", "0.1.1"], [entry["version"] for entry in index["releases"]])
        self.assertEqual(4, len(warnings))
        self.assertIn("is not JSON", warnings[0])
        self.assertIn("has no connectors-mcp-release.json", warnings[1])
        self.assertIn("not the locked", warnings[2])
        self.assertIn("v0.1.2", warnings[3])

    def test_a_failed_download_of_the_newest_release_fails_the_index(self) -> None:
        def fetch(url: str) -> bytes:
            raise release.ReleaseDownloadError(f"download {url} failed: timed out")

        releases = [github_release("0.1.1", lock("0.1.1", connector("slack", "v1.4.0")))]
        with self.assertRaisesRegex(release.ConnectorsMCPReleaseError, "cannot be indexed: .*timed out"):
            release.build_release_index(releases, fetch, lambda _message: None)

    def test_empty_index_when_nothing_is_released(self) -> None:
        self.assertEqual({"schemaVersion": release.INDEX_SCHEMA_VERSION, "releases": []}, self.build([], {})[0])


if __name__ == "__main__":
    unittest.main()
