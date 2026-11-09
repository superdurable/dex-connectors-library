# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Tests for language release selection and PyPI project-creation deferral."""

import hashlib
import json
import os
import subprocess
import tempfile
import textwrap
import unittest
import urllib.error
from pathlib import Path
from unittest.mock import patch

from script.release import language_publish


def item(name: str, language: str = "python") -> dict[str, str]:
    return {"directory": f"connectors/{name}", "tag_prefix": f"connectors/{name}/{language}/", "version": "v0.1.0",
            "package": f"dex-connector-{name}", "language": language}


def go_item(name: str, release_state: str, version: str = "v0.1.0") -> dict[str, str]:
    return {"directory": f"connectors/{name}", "manifest_path": f"connectors/{name}/connector.yaml",
            "tag_prefix": f"connectors/{name}/go/", "version": version, "display_name": name,
            "release_state": release_state}


def go_tags(entries: list[dict[str, str]]) -> list[str]:
    return [entry["tag_prefix"] + entry["version"] for entry in entries]


def names(entries: list[dict[str, str]]) -> list[str]:
    return [entry["package"].removeprefix("dex-connector-").removeprefix("@superdurable/connector-")
            for entry in entries]


def tag(name: str, language: str = "python", version: str = "v0.1.0") -> str:
    return f"connectors/{name}/{language}/{version}"


def never_on_pypi(_package: str, _version: str) -> bool:
    return False


def package_from_directory(directory: str, _tag: str) -> str:
    return "dex-connector-" + directory.removeprefix("connectors/")


def select(items: list[dict[str, str]], first_publish_limit: int, **options) -> dict[str, object]:
    options.setdefault("release_package", package_from_directory)
    return language_publish.select_release_matrices(items, first_publish_limit, **options)


class SelectReleaseMatricesTests(unittest.TestCase):
    def test_python_first_releases_go_to_github_first_and_keep_updates(self) -> None:
        items = [item("existing"), item("new-one"), item("npm-new", "typescript"), item("new-two"),
                 item("new-three"), item("completed")]
        selection = select(
            items, 2, github_release_limit=2, completed_tags={tag("completed")},
            project_exists=lambda _language, package: package in {"dex-connector-existing", "dex-connector-completed"},
            pypi_release_exists=lambda package, _version: package == "dex-connector-completed")
        self.assertEqual(["existing"], names(selection["updates"]))
        self.assertEqual(["npm-new"], names(selection["first"]))
        self.assertEqual(["new-one", "new-two"], names(selection["github_first"]))
        self.assertEqual(["github_first_release"] * 2,
                         [entry["pypi_deferred_reason"] for entry in selection["github_first"]])
        self.assertTrue(all(entry["first_publish"] for entry in selection["github_first"]))
        self.assertEqual([], selection["pypi_pending"])
        self.assertEqual(["new-three"], names(selection["deferred"]))
        self.assertEqual("github_release_cap", selection["deferred"][0]["defer_reason"])
        self.assertEqual([0, 1, 2, 3], [entry["artifact_index"] for entry in
                                        selection["updates"] + selection["first"] + selection["github_first"]])
        self.assertEqual(5, selection["pending_count"])

    def test_completed_github_release_missing_from_pypi_queues_a_pypi_only_upload(self) -> None:
        items = [item("existing-project"), item("new-project"), item("on-pypi"), item("npm-done", "typescript")]
        selection = select(
            items, 3, completed_tags={tag("existing-project"), tag("new-project"), tag("on-pypi"),
                                      tag("npm-done", "typescript"), "sdkpython/v0.8.0"},
            project_exists=lambda _language, package: package != "dex-connector-new-project",
            pypi_release_exists=lambda package, _version: package == "dex-connector-on-pypi")
        self.assertEqual(["existing-project", "new-project"], names(selection["pypi_pending"]))
        self.assertNotIn("first_publish", selection["pypi_pending"][0])
        self.assertTrue(selection["pypi_pending"][1]["first_publish"])
        self.assertTrue(all(entry["github_released"] for entry in selection["pypi_pending"]))
        self.assertEqual({"directory": "connectors/existing-project", "tag_prefix": "connectors/existing-project/python/",
                          "version": "v0.1.0", "language": "python", "github_released": True,
                          "package": "dex-connector-existing-project"}, selection["pypi_pending"][0])
        self.assertEqual([], selection["updates"] + selection["first"] + selection["github_first"])
        self.assertEqual(2, selection["pending_count"])

    def test_version_bump_keeps_an_earlier_pending_pypi_upload(self) -> None:
        bumped = item("acme")
        bumped["version"] = "v0.2.0"
        packages = []

        def release_package(directory, release_tag):
            packages.append(release_tag)
            return "dex-connector-acme-renamed" if release_tag.endswith("v0.1.0") else "dex-connector-acme"

        selection = select(
            [bumped], 3, completed_tags={tag("acme")}, release_package=release_package,
            project_exists=lambda _language, _package: False, pypi_release_exists=never_on_pypi)
        self.assertEqual([("connectors/acme/python/", "v0.1.0", "dex-connector-acme-renamed")],
                         [(entry["tag_prefix"], entry["version"], entry["package"])
                          for entry in selection["pypi_pending"]])
        self.assertEqual(["v0.2.0"], [entry["version"] for entry in selection["github_first"]])
        self.assertEqual([tag("acme")], packages)

    def test_pending_pypi_uploads_run_oldest_github_release_first(self) -> None:
        created_at = {tag("alpha"): "2026-11-06T09:00:00Z", tag("zeta"): "2026-11-05T09:00:00Z",
                      tag("beta"): "2026-11-05T09:00:00Z"}
        selection = select(
            [], 2, completed_tags={tag("alpha"), tag("zeta"), tag("beta"), tag("undated")},
            release_created_at=created_at,
            project_exists=lambda _language, _package: False, pypi_release_exists=never_on_pypi)
        self.assertEqual(["beta", "zeta"], names(selection["pypi_pending"]))
        self.assertEqual(["alpha", "undated"], names(selection["deferred"]))
        self.assertEqual({"first_publish_cap"}, {entry["defer_reason"] for entry in selection["deferred"]})

    def test_targeted_repair_uploads_only_its_connector_versions(self) -> None:
        created_at = {tag("other-a"): "2026-11-01T00:00:00Z", tag("other-b"): "2026-11-02T00:00:00Z",
                      tag("other-c"): "2026-11-03T00:00:00Z", tag("target"): "2026-11-04T00:00:00Z",
                      tag("target", version="v0.2.0"): "2026-11-05T00:00:00Z",
                      tag("target/nested"): "2026-10-01T00:00:00Z", tag("targeted"): "2026-10-01T00:00:00Z"}
        target = item("target")
        target["version"] = "v0.3.0"
        for scope, expected_pending in (
                ("connectors/target", [tag("target"), tag("target", version="v0.2.0")]),
                (None, [tag("target/nested"), tag("targeted"), tag("other-a")])):
            with self.subTest(scope=scope):
                selection = select(
                    [dict(target)], 3, completed_tags=set(created_at), release_created_at=created_at,
                    pending_upload_directory=scope,
                    project_exists=lambda _language, _package: False, pypi_release_exists=never_on_pypi)
                self.assertEqual(expected_pending, [entry["tag_prefix"] + entry["version"]
                                                    for entry in selection["pypi_pending"]])
                self.assertEqual(["v0.3.0"], [entry["version"] for entry in selection["github_first"]])
        with self.assertRaisesRegex(ValueError, "not a connector directory"):
            select([], 3, pending_upload_directory="connectors/../target")

    def test_completed_tags_accept_every_valid_connector_directory(self) -> None:
        completed = {"connectors/acme/chat_api/python/v0.1.0", "connectors/acme/python/tools/python/v0.1.0",
                     "connectors/../acme/python/v0.1.0", "connectors//acme/python/v0.1.0", "connectors/python/v0.1.0",
                     "connectors/acme/python/v0.1.0-rc.1", "sdkpython/v0.8.0", "connectors/acme/typescript/v0.1.0"}
        selection = select([], 5, completed_tags=completed, project_exists=lambda _language, _package: True,
                           pypi_release_exists=never_on_pypi)
        self.assertEqual([("connectors/acme/chat_api", "v0.1.0"), ("connectors/acme/python/tools", "v0.1.0")],
                         [(entry["directory"], entry["version"]) for entry in selection["pypi_pending"]])
        self.assertEqual("connectors/acme/chat_api/python/", selection["pypi_pending"][0]["tag_prefix"])

    def test_later_versions_of_a_queued_new_project_share_its_slot(self) -> None:
        completed = {tag("acme"), tag("acme", version="v0.2.0"), tag("other")}
        created_at = {tag("acme"): "2026-11-01T00:00:00Z", tag("acme", version="v0.2.0"): "2026-11-02T00:00:00Z",
                      tag("other"): "2026-11-03T00:00:00Z"}
        selection = select(
            [], 1, completed_tags=completed, release_created_at=created_at,
            project_exists=lambda _language, _package: False, pypi_release_exists=never_on_pypi)
        self.assertEqual(["v0.1.0", "v0.2.0"], [entry["version"] for entry in selection["pypi_pending"]])
        self.assertTrue(all(entry["first_publish"] for entry in selection["pypi_pending"]))
        self.assertEqual(["other"], names(selection["deferred"]))

    def test_pending_pypi_uploads_take_project_slots_before_new_releases(self) -> None:
        items = [item("new-release"), item("pending-one"), item("pending-two"), item("pending-three")]
        selection = select(
            items, 2, completed_tags={tag("pending-one"), tag("pending-two"), tag("pending-three")},
            project_exists=lambda _language, _package: False, pypi_release_exists=never_on_pypi)
        self.assertEqual(["pending-one", "pending-three"], names(selection["pypi_pending"]))
        self.assertEqual(["new-release"], names(selection["github_first"]))
        self.assertEqual(["pending-two"], names(selection["deferred"]))
        self.assertEqual("first_publish_cap", selection["deferred"][0]["defer_reason"])
        self.assertTrue(selection["deferred"][0]["github_released"])

    def test_pypi_lookup_failure_for_completed_release_is_reported_without_an_upload(self) -> None:
        def pypi_release_exists(_package, _version):
            raise RuntimeError("PyPI unavailable")

        selection = select(
            [item("completed")], 3, completed_tags={tag("completed")},
            project_exists=lambda _language, _package: False, pypi_release_exists=pypi_release_exists)
        self.assertEqual([], selection["pypi_pending"])
        self.assertEqual([tag("completed")], [entry["tag_prefix"] + entry["version"] for entry in selection["deferred"]])
        self.assertEqual("registry_lookup_failure", selection["deferred"][0]["defer_reason"])
        self.assertTrue(selection["deferred"][0]["github_released"])
        self.assertEqual(1, selection["pending_count"])

    def test_reads_package_from_the_release_tag_and_rejects_unreachable_tags(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            repository = Path(temporary)

            def git(*arguments: str) -> None:
                subprocess.run(("git", *arguments), cwd=repository, check=True, capture_output=True)

            git("init", "-q", "-b", "main")
            git("config", "user.email", "test@example.com")
            git("config", "user.name", "Test")
            pyproject = repository / "connectors/acme/python/pyproject.toml"
            pyproject.parent.mkdir(parents=True)
            pyproject.write_text('[project]\nname = "dex-connector-acme-old"\n', encoding="utf-8")
            git("add", ".")
            git("commit", "-q", "-m", "release")
            git("tag", tag("acme"))
            pyproject.write_text('[project]\nname = "dex-connector-acme"\n', encoding="utf-8")
            git("commit", "-q", "-am", "rename")
            git("checkout", "-q", "-b", "side")
            git("commit", "-q", "--allow-empty", "-m", "side")
            git("tag", tag("acme", version="v0.9.0"))
            git("checkout", "-q", "main")
            previous_directory = os.getcwd()
            os.chdir(repository)
            try:
                self.assertEqual("dex-connector-acme-old",
                                 language_publish.read_release_package("connectors/acme", tag("acme")))
                with self.assertRaisesRegex(RuntimeError, "not reachable"):
                    language_publish.read_release_package("connectors/acme", tag("acme", version="v0.9.0"))
            finally:
                os.chdir(previous_directory)

    def test_rejects_zero_limits(self) -> None:
        with self.assertRaisesRegex(ValueError, "PyPI first publish limit must be positive"):
            language_publish.select_release_matrices([], 0)
        with self.assertRaisesRegex(ValueError, "GitHub first release limit must be positive"):
            language_publish.select_release_matrices([], 1, github_release_limit=0)

    def test_lookup_failure_defers_one_language_without_stopping_others(self) -> None:
        def project_exists(_language, package):
            if package == "dex-connector-unavailable":
                raise RuntimeError("registry unavailable")
            return False

        selection = language_publish.select_release_matrices(
            [item("unavailable"), item("available")], 1, project_exists=project_exists)
        self.assertEqual(["dex-connector-available"], [entry["package"] for entry in selection["github_first"]])
        self.assertEqual(["dex-connector-unavailable"], [entry["package"] for entry in selection["deferred"]])
        self.assertEqual("registry_lookup_failure", selection["deferred"][0]["defer_reason"])

    def test_caps_github_first_releases_by_default(self) -> None:
        selection = language_publish.select_release_matrices(
            [item(f"project-{index}") for index in range(300)], 3,
            project_exists=lambda _language, _package: False)
        self.assertEqual(language_publish.GITHUB_FIRST_RELEASE_LIMIT, len(selection["github_first"]))
        self.assertEqual(300 - language_publish.GITHUB_FIRST_RELEASE_LIMIT, len(selection["deferred"]))

    def test_defers_after_selected_matrix_reaches_256(self) -> None:
        selection = language_publish.select_release_matrices(
            [item(f"existing-{index}") for index in range(300)] + [item("npm-new", "typescript")], 3,
            project_exists=lambda _language, package: package.startswith("dex-connector-existing"))
        self.assertEqual(256, len(selection["updates"]))
        self.assertEqual([], selection["first"])
        self.assertEqual(45, len(selection["deferred"]))
        self.assertEqual({"matrix_limit"}, {entry["defer_reason"] for entry in selection["deferred"]})

    def test_writes_github_first_and_pypi_pending_outputs(self) -> None:
        selection = select(
            [item("new"), item("pending")], 3, completed_tags={tag("pending")},
            project_exists=lambda _language, _package: False, pypi_release_exists=never_on_pypi)
        with tempfile.TemporaryDirectory() as temporary:
            output_path = Path(temporary) / "output"
            language_publish.write_selection_outputs(output_path, selection, {"selected": [], "deferred": []})
            outputs = dict(line.split("=", 1) for line in output_path.read_text(encoding="utf-8").splitlines())
        self.assertEqual("1", outputs["github_first_count"])
        self.assertEqual("1", outputs["pypi_pending_count"])
        self.assertEqual("1", outputs["language_count"])
        self.assertEqual(["dex-connector-new"],
                         [entry["package"] for entry in json.loads(outputs["language_matrix"])["include"]])
        self.assertEqual(["dex-connector-pending"],
                         [entry["package"] for entry in json.loads(outputs["pypi_pending_matrix"])["include"]])


class SelectGoReleasesTests(unittest.TestCase):
    def test_caps_pending_releases_with_updates_before_first_releases_in_catalog_order(self) -> None:
        items = [go_item("alpha", "first_release"), go_item("bravo", "update", "v0.2.0"),
                 go_item("charlie", "first_release"), go_item("delta", "update", "v1.1.0"),
                 go_item("echo", "first_release")]
        selection = language_publish.select_go_releases(items, 3, set())
        self.assertEqual(["connectors/bravo/go/v0.2.0", "connectors/delta/go/v1.1.0", "connectors/alpha/go/v0.1.0"],
                         go_tags(selection["selected"]))
        self.assertEqual(["connectors/charlie/go/v0.1.0", "connectors/echo/go/v0.1.0"], go_tags(selection["deferred"]))
        self.assertEqual({"go_release_cap"}, {entry["defer_reason"] for entry in selection["deferred"]})
        self.assertTrue(all("release_state" not in entry for entry in selection["selected"] + selection["deferred"]))
        self.assertTrue(all("defer_reason" not in entry for entry in selection["selected"]))

    def test_selection_is_stable_across_runs(self) -> None:
        def items() -> list[dict[str, str]]:
            return [go_item(f"connector-{index:03}", "first_release") for index in range(100)]

        first_run = language_publish.select_go_releases(items(), language_publish.GO_RELEASE_LIMIT, set())
        second_run = language_publish.select_go_releases(items(), language_publish.GO_RELEASE_LIMIT, set())
        self.assertEqual(go_tags(first_run["selected"]), go_tags(second_run["selected"]))
        self.assertEqual(15, len(first_run["selected"]))
        self.assertEqual(85, len(first_run["deferred"]))
        self.assertEqual("connectors/connector-000/go/v0.1.0", go_tags(first_run["selected"])[0])

    def test_tagged_versions_are_never_deferred_and_only_incomplete_ones_take_slots(self) -> None:
        items = [go_item("complete", "tagged"), go_item("incomplete", "tagged"), go_item("update", "update", "v0.2.0"),
                 go_item("new", "first_release")]
        selection = language_publish.select_go_releases(items, 2, {"connectors/complete/go/v0.1.0"})
        self.assertEqual(["connectors/complete/go/v0.1.0", "connectors/incomplete/go/v0.1.0",
                          "connectors/update/go/v0.2.0"], go_tags(selection["selected"]))
        self.assertEqual(["connectors/new/go/v0.1.0"], go_tags(selection["deferred"]))

        tagged = [go_item(f"tagged-{index}", "tagged") for index in range(4)]
        selection = language_publish.select_go_releases(tagged + [go_item("new", "first_release")], 2, None)
        self.assertEqual(4, len(selection["selected"]), "a tagged version beyond the limit still runs")
        self.assertEqual(["connectors/new/go/v0.1.0"], go_tags(selection["deferred"]))

    def test_rejects_invalid_limits_and_unknown_states(self) -> None:
        for limit in (0, language_publish.MAXIMUM_GITHUB_MATRIX_JOBS + 1):
            with self.subTest(limit=limit), self.assertRaisesRegex(ValueError, "Go release limit"):
                language_publish.select_go_releases([], limit, set())
        with self.assertRaisesRegex(ValueError, "unknown release_state"):
            language_publish.select_go_releases([{"directory": "connectors/acme"}], 1, set())

    def test_language_packages_wait_for_their_deferred_go_release(self) -> None:
        lookups = []

        def project_exists(language: str, package: str) -> bool:
            lookups.append(package)
            return False

        selection = select(
            [item("released"), item("deferred"), item("deferred", "typescript"), item("released", "typescript")], 3,
            github_release_limit=1, deferred_go_directories=frozenset({"connectors/deferred"}),
            project_exists=project_exists, pypi_release_exists=never_on_pypi)
        self.assertEqual(["released"], names(selection["github_first"]))
        self.assertEqual(["released"], names(selection["first"]))
        self.assertEqual([("connectors/deferred/python/", "go_release_deferred"),
                          ("connectors/deferred/typescript/", "go_release_deferred")],
                         [(entry["tag_prefix"], entry["defer_reason"]) for entry in selection["deferred"]])
        self.assertNotIn("dex-connector-deferred", lookups)
        self.assertEqual(4, selection["pending_count"])

    def test_select_command_writes_capped_go_matrix_and_defers_its_languages(self) -> None:
        go_items = [go_item(f"connector-{index:03}", "first_release") for index in range(100)]
        language_items = [item(f"connector-{index:03}", language)
                          for index in range(100) for language in ("python", "typescript")]
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            matrix_output = directory / "matrix"
            github_output = directory / "output"
            summary_path = directory / "summary.md"
            matrix_output.write_text(f"matrix={json.dumps({'include': go_items})}\ncount=100\n"
                                     f"language_matrix={json.dumps({'include': language_items})}\n"
                                     "language_count=200\n", encoding="utf-8")
            arguments = ["language_publish.py", "select", "--matrix-output", str(matrix_output),
                         "--github-output", str(github_output), "--go-release-limit", "15",
                         "--first-publish-limit", "3", "--github-release-limit", "15"]
            with patch.object(language_publish, "list_completed_go_tags", return_value=set()), \
                 patch.object(language_publish, "list_completed_language_tags", return_value=set()), \
                 patch.object(language_publish, "has_registry_project", return_value=False), \
                 patch.object(language_publish, "select_pending_pypi_uploads", return_value=[]), \
                 patch.object(language_publish.sys, "argv", arguments), \
                 patch.dict(os.environ, {"LANGUAGE_SELECTION_PATH": str(directory / "selection.json"),
                                         "GITHUB_STEP_SUMMARY": str(summary_path)}):
                self.assertEqual(0, language_publish.main())
            outputs = dict(line.split("=", 1) for line in github_output.read_text(encoding="utf-8").splitlines())
            selection = json.loads((directory / "selection.json").read_text(encoding="utf-8"))
            summary = summary_path.read_text(encoding="utf-8")
        go_matrix = json.loads(outputs["matrix"])["include"]
        self.assertEqual(("15", "85"), (outputs["count"], outputs["go_deferred_count"]))
        self.assertEqual([f"connectors/connector-{index:03}" for index in range(15)],
                         [entry["directory"] for entry in go_matrix])
        self.assertTrue(all("release_state" not in entry for entry in go_matrix))
        self.assertEqual(("15", "15", "30"), (outputs["github_first_count"], outputs["first_count"],
                                              outputs["language_count"]))
        released_directories = {entry["directory"] for entry in go_matrix}
        self.assertTrue(all(entry["directory"] in released_directories
                            for entry in json.loads(outputs["language_matrix"])["include"]))
        self.assertEqual("170", outputs["deferred_count"])
        self.assertEqual({"go_release_deferred"}, {entry["defer_reason"] for entry in selection["deferred"]})
        self.assertEqual(85, len(selection["go_deferred"]))
        self.assertIn("- deferred (go_release_cap): 85", summary)


class PyPIReleaseLookupTests(unittest.TestCase):
    def test_requires_both_wheel_and_sdist(self) -> None:
        urls = []

        def read(url, _description):
            urls.append(url)
            return {"urls": [{"packagetype": "bdist_wheel"}]}

        self.assertFalse(language_publish.has_pypi_release_files("dex-connector-acme", "v0.1.0", read=read))
        self.assertEqual(["https://pypi.org/pypi/dex-connector-acme/0.1.0/json"], urls)
        self.assertTrue(language_publish.has_pypi_release_files(
            "dex-connector-acme", "v0.1.0",
            read=lambda _url, _description: {"urls": [{"packagetype": "bdist_wheel"}, {"packagetype": "sdist"}]}))
        self.assertFalse(language_publish.has_pypi_release_files(
            "dex-connector-acme", "v0.1.0", read=lambda _url, _description: None))

    def test_catalog_tags_check_only_the_latest_completed_python_release(self) -> None:
        checked = []

        def release_exists(package, version):
            checked.append((package, version))
            if package == "dex-connector-broken":
                raise RuntimeError("PyPI unavailable")
            return package == "dex-connector-published"

        completed = {"connectors/published/python/v0.1.0", "connectors/published/python/v0.10.0",
                     "connectors/published/python/v0.9.0", "connectors/pending/python/v0.1.0",
                     "connectors/broken/python/v0.1.0", "connectors/npm/typescript/v0.1.0"}
        published = language_publish.pypi_published_language_tags(
            [item("published"), item("pending"), item("broken"), item("npm", "typescript"), item("unreleased")],
            completed, release_exists=release_exists)
        self.assertEqual({"connectors/published/python/v0.10.0"}, published)
        self.assertEqual([("dex-connector-published", "v0.10.0"), ("dex-connector-pending", "v0.1.0"),
                          ("dex-connector-broken", "v0.1.0")], checked)


class ReleaseNotesTests(unittest.TestCase):
    def pending_block(self) -> str:
        return language_publish.pending_pypi_notes_block(
            "superdurable/dex-connectors-library", "connectors/calendly/python/v0.1.0", "dex-connector-calendly",
            "v0.1.0", "connectors/calendly", "dex_connector_calendly-0.1.0-py3-none-any.whl")

    def test_pending_block_gives_version_pinned_github_installs(self) -> None:
        block = self.pending_block()
        self.assertIn("pip install https://github.com/superdurable/dex-connectors-library/releases/download/"
                      "connectors%2Fcalendly%2Fpython%2Fv0.1.0/dex_connector_calendly-0.1.0-py3-none-any.whl", block)
        self.assertIn('pip install "dex-connector-calendly @ git+https://github.com/superdurable/dex-connectors-library'
                      '@connectors/calendly/python/v0.1.0#subdirectory=connectors/calendly/python"', block)
        self.assertIn("If `pip install dex-connector-calendly==0.1.0` cannot find this version yet", block)
        self.assertTrue(block.startswith(language_publish.PENDING_PYPI_NOTES_START))

    def test_pending_block_is_idempotent_and_replaced_after_upload(self) -> None:
        notes = "## Breaking Changes\n\nNone.\n\n## Changes\n\n- [Add Calendly](https://example.com/1)\n"
        pending = language_publish.add_pending_pypi_notes(notes, self.pending_block())
        self.assertEqual(pending, language_publish.add_pending_pypi_notes(pending, self.pending_block()))
        self.assertEqual(1, pending.count(language_publish.PENDING_PYPI_NOTES_START))
        published = language_publish.published_pypi_notes(pending, "dex-connector-calendly", "v0.1.0")
        self.assertNotIn("pypi-upload-pending", published)
        self.assertNotIn("git+https://", published)
        self.assertTrue(published.startswith(notes))
        self.assertTrue(published.endswith("## Install\n\n```sh\npip install dex-connector-calendly==0.1.0\n```\n"))
        self.assertEqual(published, language_publish.published_pypi_notes(published, "dex-connector-calendly", "v0.1.0"))

    def test_published_notes_leave_notes_without_pending_block_unchanged(self) -> None:
        notes = "## Changes\n\n- Something.\n"
        self.assertEqual(notes, language_publish.published_pypi_notes(notes, "dex-connector-calendly", "v0.1.0"))

    def test_rejects_unterminated_pending_block(self) -> None:
        with self.assertRaisesRegex(ValueError, "unterminated"):
            language_publish.published_pypi_notes(
                "notes\n" + language_publish.PENDING_PYPI_NOTES_START + "\n", "dex-connector-calendly", "v0.1.0")


class VerifyReleaseArchivesTests(unittest.TestCase):
    package = "dex-connector-calendly"
    release_tag = "connectors/calendly/python/v0.1.0"
    wheel = "dex_connector_calendly-0.1.0-py3-none-any.whl"
    sdist = "dex_connector_calendly-0.1.0.tar.gz"

    source_sha = "1" * 40
    _omitted = object()

    def write_release(self, directory: Path, source_sha: object = _omitted) -> None:
        digests = {}
        for name, contents in ((self.wheel, b"wheel bytes"), (self.sdist, b"sdist bytes")):
            (directory / name).write_bytes(contents)
            digests[name] = hashlib.sha256(contents).hexdigest()
            (directory / f"{name}.sha256").write_text(f"{digests[name]}  {name}\n", encoding="utf-8")
        document = {"language": "python", "package": self.package, "version": "v0.1.0",
                    "tag": self.release_tag, "sourceSha": self.source_sha, "artifacts": digests}
        if source_sha is None:
            del document["sourceSha"]
        elif source_sha is not self._omitted:
            document["sourceSha"] = source_sha
        manifest = json.dumps(document).encode("utf-8")
        (directory / "language-release.json").write_bytes(manifest)
        (directory / "language-release.json.sha256").write_text(
            f"{hashlib.sha256(manifest).hexdigest()}  language-release.json\n", encoding="utf-8")

    def verify(self, directory: Path, version: str = "v0.1.0", source_sha: str = source_sha) -> list[Path]:
        return language_publish.verify_release_archives(directory, self.release_tag, self.package, version,
                                                        source_sha)

    def test_binds_the_manifest_source_to_the_tag_commit(self) -> None:
        for manifest_source_sha, message in ((None, "sourceSha must be a full commit SHA"),
                                             ("abc", "sourceSha must be a full commit SHA"),
                                             ("2" * 40, "differs from tag commit")):
            with self.subTest(manifest_source_sha=manifest_source_sha), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                self.write_release(directory, manifest_source_sha)
                with self.assertRaisesRegex(ValueError, message):
                    self.verify(directory)
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            self.write_release(directory)
            # A tag moved after release peels to a different commit than the recorded source.
            with self.assertRaisesRegex(ValueError, "differs from tag commit"):
                self.verify(directory, source_sha="3" * 40)
            with self.assertRaisesRegex(ValueError, "expected source SHA"):
                self.verify(directory, source_sha="")

    def test_returns_wheel_and_sdist_that_match_the_release_manifest(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            self.write_release(directory)
            self.assertEqual([directory / self.wheel, directory / self.sdist], self.verify(directory))

    def test_rejects_changed_archive_bytes(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            self.write_release(directory)
            (directory / self.wheel).write_bytes(b"rebuilt wheel")
            with self.assertRaisesRegex(ValueError, "SHA-256 differs"):
                self.verify(directory)

    def test_rejects_changed_manifest_identity_or_digest_file(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            self.write_release(directory)
            with self.assertRaisesRegex(ValueError, "identity"):
                self.verify(directory, "v0.2.0")
            (directory / "language-release.json.sha256").write_text("0  language-release.json\n", encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "language-release.json.sha256"):
                self.verify(directory)

    def test_rejects_unexpected_files(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            self.write_release(directory)
            (directory / "language-release.complete").write_text(self.release_tag, encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "differs from the release manifest"):
                self.verify(directory)


class ReleaseMutationTests(unittest.TestCase):
    built = "a" * 40
    trusted = "b" * 40

    def test_complete_release_is_never_modified_even_by_a_stale_build(self) -> None:
        self.assertEqual("complete", language_publish.release_mutation_state(
            ["x.whl", "language-release.complete"], self.built, self.trusted, "c" * 40))

    def test_rejects_stale_build_or_moved_tag_before_repair(self) -> None:
        with self.assertRaisesRegex(ValueError, "stale publish job"):
            language_publish.release_mutation_state(["x.whl"], self.built, self.trusted, "")
        with self.assertRaisesRegex(ValueError, "remote tag commit"):
            language_publish.release_mutation_state(None, self.trusted, self.trusted, "c" * 40)
        self.assertEqual("absent", language_publish.release_mutation_state(None, self.trusted, self.trusted, ""))
        self.assertEqual("incomplete", language_publish.release_mutation_state(
            ["x.whl"], self.trusted, self.trusted, self.trusted))

    def test_existing_archives_must_equal_rebuilt_archives(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            existing, dist = Path(temporary) / "existing", Path(temporary) / "dist"
            existing.mkdir()
            dist.mkdir()
            (existing / "a.whl").write_bytes(b"same")
            (dist / "a.whl").write_bytes(b"same")
            language_publish.verify_existing_archives(existing, dist)
            (dist / "a.whl").write_bytes(b"rebuilt")
            with self.assertRaisesRegex(ValueError, "different a.whl"):
                language_publish.verify_existing_archives(existing, dist)
            (existing / "b.tar.gz").write_bytes(b"other")
            with self.assertRaisesRegex(ValueError, "did not produce"):
                language_publish.verify_existing_archives(existing, dist)


class RegistryLookupTests(unittest.TestCase):
    def test_retries_a_registry_5xx_before_treating_404_as_absent(self) -> None:
        responses = [urllib.error.HTTPError("https://pypi.org", 503, "busy", {}, None),
                     urllib.error.HTTPError("https://pypi.org", 404, "missing", {}, None)]
        delays = []
        with patch.object(language_publish.urllib.request, "urlopen", side_effect=responses):
            self.assertFalse(language_publish.has_registry_url("https://pypi.org/project", "project", sleep=delays.append))
        for response in responses:
            response.close()
        self.assertEqual([1], delays)

    def test_reads_completed_language_tags_from_one_inventory(self) -> None:
        inventory = {"connectors/one/python/v0.1.0": ["language-release.complete"],
                     "connectors/two/python/v0.1.0": ["language-release.json"]}
        with patch.object(language_publish, "release_inventory", return_value=inventory) as fetch:
            self.assertEqual({"connectors/one/python/v0.1.0"}, language_publish.list_completed_language_tags())
        fetch.assert_called_once_with()

    def test_release_inventory_failure_retains_pending_go_matrix(self) -> None:
        workflow = (Path(__file__).resolve().parents[2] / ".github/workflows/release-connector.yml").read_text()
        inventory_step = workflow.split("      - name: Resolve release inventory once for planning and release jobs\n", 1)[1]
        self.assertIn("continue-on-error: true", inventory_step.split("      - name:", 1)[0])
        self.assertIn('printf \'{"_inventory_unavailable":true}', inventory_step)
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            matrix_output = directory / "matrix"
            github_output = directory / "output"
            matrix_output.write_text("matrix=" + json.dumps({"include": [go_item("acme", "first_release")]})
                                     + "\ncount=1\n"
                                     + "language_matrix=" + json.dumps({"include": [item("acme")]}) + "\n", encoding="utf-8")
            arguments = ["language_publish.py", "select", "--matrix-output", str(matrix_output),
                         "--github-output", str(github_output), "--first-publish-limit", "3"]
            with patch.object(language_publish, "list_completed_language_tags", side_effect=RuntimeError("rate limited")), \
                 patch.object(language_publish, "list_completed_go_tags", side_effect=RuntimeError("rate limited")), \
                 patch.object(language_publish.sys, "argv", arguments), \
                 patch.dict(os.environ, {"LANGUAGE_SELECTION_PATH": str(directory / "selection.json")}):
                self.assertEqual(0, language_publish.main())
            output = github_output.read_text(encoding="utf-8")
            self.assertIn("count=1\n", output)
            self.assertIn("deferred_count=1\n", output)
            selection = json.loads((directory / "selection.json").read_text(encoding="utf-8"))
            self.assertEqual("release_inventory_unavailable", selection["deferred"][0]["defer_reason"])

    def test_unavailable_inventory_marker_defers_languages(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            inventory_path = Path(temporary) / "inventory.json"
            inventory_path.write_text('{"_inventory_unavailable":true}')
            with patch.dict(os.environ, {"GH_RELEASE_INVENTORY_PATH": str(inventory_path)}):
                with self.assertRaisesRegex(RuntimeError, "inventory is unavailable"):
                    language_publish.list_completed_language_tags()

    def test_project_limit_artifact_lookup_paginates(self) -> None:
        attempts = []

        def fetch(url):
            attempts.append(url)
            if len(attempts) == 1:
                return {"artifacts": [{"name": "another-artifact"}]}, '<https://api.github.com/next>; rel="next"'
            return {"artifacts": [{"name": "pypi-project-limit-42"}]}, ""

        self.assertTrue(language_publish.has_run_project_limit_artifact(
            "superdurable/dex-connectors-library", "42", fetch=fetch))
        self.assertEqual("https://api.github.com/next", attempts[1])


class PublishPypiTests(unittest.TestCase):
    def test_project_creation_limit_defers_after_bounded_retries(self) -> None:
        attempts = []
        delays = []

        def run(command, **_kwargs):
            attempts.append(command)
            return subprocess.CompletedProcess(command, 1, "", "429 Too many new projects created")

        status = language_publish.publish_pypi(["wheel.whl"], delays=(1, 2), run=run, sleep=delays.append)
        self.assertEqual("deferred", status)
        self.assertEqual(3, len(attempts))
        self.assertEqual([1, 2], delays)

    def test_real_upload_error_fails_without_retry(self) -> None:
        calls = []

        def run(command, **_kwargs):
            calls.append(command)
            return subprocess.CompletedProcess(command, 1, "", "403 invalid credentials")

        with self.assertRaisesRegex(RuntimeError, "403 invalid credentials"):
            language_publish.publish_pypi(["wheel.whl"], delays=(1,), run=run, sleep=lambda _: None)
        self.assertEqual(1, len(calls))

    def test_success_after_limit_retry(self) -> None:
        responses = iter((subprocess.CompletedProcess([], 1, "", "429 Too many new projects created"),
                          subprocess.CompletedProcess([], 0, "uploaded", "")))
        self.assertEqual("published", language_publish.publish_pypi(["wheel.whl"], delays=(1,),
                                                                  run=lambda *_args, **_kwargs: next(responses),
                                                                  sleep=lambda _: None))


class SummaryTests(unittest.TestCase):
    def test_reports_published_deferred_and_missing_results(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            (directory / "release-result.json").write_text(json.dumps({
                "tag": "connectors/published/python/v0.1.0", "status": "published"}), encoding="utf-8")
            (directory / "throttled").mkdir()
            (directory / "throttled" / "release-result.json").write_text(json.dumps({
                "tag": "connectors/throttled/python/v0.1.0", "status": "deferred",
                "reason": "project_creation_limit"}), encoding="utf-8")
            capped = item("capped")
            capped["defer_reason"] = "first_publish_cap"
            summary = language_publish.summarize(
                [item("published"), item("throttled"), item("missing")], directory, [capped])
        self.assertEqual(["connectors/published/python/v0.1.0"], summary["published"])
        self.assertEqual(["connectors/capped/python/v0.1.0", "connectors/throttled/python/v0.1.0"], summary["deferred"])
        self.assertEqual({"connectors/capped/python/v0.1.0": "first_publish_cap",
                          "connectors/throttled/python/v0.1.0": "project_creation_limit"}, summary["deferred_reasons"])
        self.assertEqual(["connectors/missing/python/v0.1.0"], summary["failed"])
        self.assertEqual(3, summary["pending_count"])

    def test_reports_github_releases_whose_pypi_upload_is_pending(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            for name, status, reason in (("github-first", "pypi_pending", "github_first_release"),
                                         ("uploaded", "published", ""),
                                         ("throttled", "pypi_pending", "project_creation_limit")):
                (directory / name).mkdir()
                (directory / name / "release-result.json").write_text(json.dumps({
                    "tag": tag(name), "status": status, "reason": reason}), encoding="utf-8")
            capped = item("capped")
            capped.update(defer_reason="first_publish_cap", github_released=True)
            uploaded, throttled = item("uploaded"), item("throttled")
            uploaded["github_released"] = throttled["github_released"] = True
            summary = language_publish.summarize(
                [item("github-first"), uploaded, throttled], directory, [capped])
            summary_path = directory / "summary.md"
            language_publish.write_step_summary(summary_path, summary)
            step_summary = summary_path.read_text(encoding="utf-8")
        self.assertEqual([tag("uploaded")], summary["published"])
        self.assertEqual([tag("capped"), tag("github-first"), tag("throttled")], summary["pypi_pending"])
        self.assertEqual({tag("capped"): "first_publish_cap", tag("github-first"): "github_first_release",
                          tag("throttled"): "project_creation_limit"}, summary["pypi_pending_reasons"])
        self.assertEqual([], summary["deferred"])
        self.assertEqual(3, summary["pending_count"])
        self.assertIn(f"- pypi_pending: 3 — {tag('capped')} (first_publish_cap)", step_summary)
        self.assertIn("- deferred: 0 — none", step_summary)


class WorkflowTests(unittest.TestCase):
    workflow = (Path(__file__).resolve().parents[2] / ".github/workflows/release-connector.yml").read_text(
        encoding="utf-8")

    def job(self, name: str) -> str:
        return self.workflow.split(f"\n  {name}:\n", 1)[1].split("\n\n  ", 1)[0]

    def test_pypi_only_upload_reuses_github_release_bytes(self) -> None:
        upload_job = self.job("upload-pending-pypi")
        self.assertIn("gh release download", upload_job)
        self.assertIn("verify-release-archives", upload_job)
        self.assertIn("verify-language-artifacts", upload_job)
        self.assertIn("--require-all --wait-seconds 120", upload_job)
        for rebuild_marker in ("uv build", "language-manifest", "--clobber", "component_release.py notes",
                               "language-release.complete"):
            self.assertNotIn(rebuild_marker, upload_job)
        self.assertIn("max-parallel: 1", upload_job)

    def test_new_release_jobs_gate_the_summary_and_catalog(self) -> None:
        for job in ("summarize-language", "build-catalog"):
            needs = self.job(job).split("needs: [", 1)[1].split("]", 1)[0]
            self.assertIn("publish-language-github-first", needs)
            self.assertIn("upload-pending-pypi", needs)
        catalog_job = self.job("build-catalog")
        self.assertIn("needs.plan.outputs.github_first_count == '0'", catalog_job)
        self.assertNotIn("needs.upload-pending-pypi.result", catalog_job,
                         "the catalog reads PyPI itself, so a failed upload must not block deployment")
        self.assertIn("--pypi-published-language-tags", catalog_job)

    def test_github_first_release_writes_completion_marker_with_pending_notes(self) -> None:
        publish_steps = self.job("publish-language")
        self.assertIn("pending-pypi-notes", publish_steps)
        create_step = publish_steps.split("      - name: Create or repair language release\n", 1)[1].split("      - name:", 1)[0]
        complete_step = publish_steps.split("      - name: Mark language release complete\n", 1)[1].split("      - name:", 1)[0]
        for step in (create_step, complete_step):
            self.assertIn("if: steps.state.outputs.release_required == 'true' && "
                          "steps.guard.outputs.release_state != 'complete'\n", step)
        self.assertIn("matrix: ${{ fromJSON(needs.plan.outputs.github_first_matrix) }}",
                      self.job("publish-language-github-first"))

    def test_release_result_reports_pypi_pending_after_github_completion(self) -> None:
        step = self.job("publish-language").split("      - name: Record language release result\n", 1)[1].split(
            "      - uses:", 1)[0]
        script = textwrap.dedent(step.split("        run: |\n", 1)[1])
        cases = (
            ({"PYPI_STATUS": "deferred", "PYPI_DEFERRED_REASON": "github_first_release", "COMPLETE_STATUS": "success"},
             ("pypi_pending", "github_first_release")),
            ({"PYPI_STATUS": "deferred", "PYPI_DEFERRED_REASON": "", "COMPLETE_STATUS": "success"},
             ("pypi_pending", "project_creation_limit")),
            ({"PYPI_STATUS": "deferred", "PYPI_DEFERRED_REASON": "github_first_release", "COMPLETE_STATUS": "failure"},
             ("failed", "")),
            ({"PYPI_STATUS": "", "PYPI_DEFERRED_REASON": "", "COMPLETE_STATUS": "success"}, ("published", "")),
            ({"PYPI_STATUS": "deferred", "PYPI_DEFERRED_REASON": "github_first_release", "COMPLETE_STATUS": "skipped",
              "RELEASE_STATE": "complete"}, ("published", "already_complete")),
        )
        for environment, expected in cases:
            with self.subTest(environment=environment), tempfile.TemporaryDirectory() as temporary:
                subprocess.run(("bash", "-e", "-o", "pipefail", "-c", script), check=True, env=dict(
                    os.environ, RUNNER_TEMP=temporary, JOB_STATUS="success", RELEASE_REQUIRED="true",
                    TAG_PREFIX="connectors/acme/python/", RELEASE_VERSION="v0.1.0", **environment))
                result = json.loads((Path(temporary) / "release-result" / "release-result.json").read_text())
                self.assertEqual(expected, (result["status"], result["reason"]))

    def test_only_targeted_repair_scopes_pending_pypi_uploads(self) -> None:
        plan_job = self.job("plan")
        step = plan_job.split("      - name: Build pending or targeted repair matrix\n", 1)[1].split("      - ", 1)[0]
        script = textwrap.dedent(step.split("        run: |\n", 1)[1])
        cases = ((("workflow_dispatch", "connectors/target", "false"), ["--pending-upload-directory", "connectors/target"]),
                 (("schedule", "", "false"), []), (("workflow_dispatch", "", "true"), []), (("push", "", "false"), []))
        for (event_name, repair_directory, drain), expected_scope in cases:
            with self.subTest(event_name=event_name, drain=drain), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                bin_directory = directory / "bin"
                bin_directory.mkdir()
                for command in ("go", "python3"):
                    (bin_directory / command).write_text(f'#!/bin/sh\nprintf "%s\\n" "$@" > "$LOG_DIRECTORY/{command}"\n')
                    (bin_directory / command).chmod(0o755)
                subprocess.run(("bash", "-e", "-o", "pipefail", "-c", script), check=True, env=dict(
                    os.environ, PATH=f"{bin_directory}:{os.environ['PATH']}", LOG_DIRECTORY=temporary,
                    EVENT_NAME=event_name, REPAIR_DIRECTORY=repair_directory, DRAIN_FIRST_PUBLISHES=drain,
                    SOURCE_SHA="a" * 40, RUNNER_TEMP=temporary, GITHUB_OUTPUT=str(directory / "output"),
                    FIRST_PUBLISH_LIMIT="3", PYTHON_GITHUB_FIRST_RELEASE_LIMIT="15", GO_RELEASE_LIMIT="15"))
                select_arguments = (directory / "python3").read_text().splitlines()
                self.assertEqual("select", select_arguments[1])
                self.assertEqual("15", select_arguments[select_arguments.index("--go-release-limit") + 1])
                scope = select_arguments[select_arguments.index("--github-release-limit") + 2:]
                self.assertEqual(expected_scope, scope)

    def run_guard(self, temporary: Path, *, release_assets: list[str] | None, built_source_sha: str | None,
                  existing_archive: bytes | None = None, pypi_status: str = "deferred") -> subprocess.CompletedProcess:
        """Run the guard step with fake release lookup and gh; a None built source matches the fixture HEAD."""
        publish_job = self.job("publish-language")
        step = publish_job.split("      - name: Check the live release before changing it\n", 1)[1].split(
            "      - name:", 1)[0]
        script = textwrap.dedent(step.split("        run: |\n", 1)[1])
        repository = temporary / "repository"
        repository.mkdir()
        for arguments in (("init", "-q"), ("-c", "user.email=t@example.com", "-c", "user.name=T", "commit", "-q",
                                           "--allow-empty", "-m", "source"), ("remote", "add", "origin", str(repository))):
            subprocess.run(("git", *arguments), cwd=repository, check=True, capture_output=True)
        trusted_source_sha = subprocess.run(("git", "rev-parse", "HEAD"), cwd=repository, check=True,
                                            capture_output=True, text=True).stdout.strip()
        runner_temp = temporary / "runner"
        dist = runner_temp / "language-work" / "dist"
        dist.mkdir(parents=True)
        (dist / "dex_connector_acme-0.1.0-py3-none-any.whl").write_bytes(b"rebuilt wheel")
        (runner_temp / "language-work" / "plan.json").write_text(
            json.dumps({"source_sha": trusted_source_sha if built_source_sha is None else built_source_sha}))
        (runner_temp / "trusted-language-plan.json").write_text(json.dumps({"source_sha": trusted_source_sha}))
        (runner_temp / "language_publish.py").write_text(Path(language_publish.__file__).read_text())
        inventory_source = (Path(language_publish.__file__).parent / "github_release_inventory.py").read_text()
        entry_point = 'if __name__ == "__main__":\n    main()\n'
        self.assertIn(entry_point, inventory_source)
        (runner_temp / "github_release_inventory.py").write_text(inventory_source.replace(
            entry_point, f'if __name__ == "__main__":\n    print({json.dumps(json.dumps(release_assets))})\n'))
        existing = temporary / "existing"
        existing.mkdir()
        if existing_archive is not None:
            (existing / "dex_connector_acme-0.1.0-py3-none-any.whl").write_bytes(existing_archive)
        bin_directory = temporary / "bin"
        bin_directory.mkdir()
        gh = bin_directory / "gh"
        gh.write_text('#!/bin/sh\necho "$@" >> "$GH_LOG"\nwhile [ $# -gt 0 ]; do\n'
                      '  if [ "$1" = --dir ]; then cp "$FAKE_EXISTING"/* "$2"/ 2>/dev/null || true; fi\n'
                      '  shift\ndone\n')
        gh.chmod(0o755)
        return subprocess.run(("bash", "-e", "-o", "pipefail", "-c", script), cwd=repository, capture_output=True,
                              text=True, env=dict(
                                  os.environ, PATH=f"{bin_directory}:{os.environ['PATH']}", RUNNER_TEMP=str(runner_temp),
                                  GITHUB_OUTPUT=str(temporary / "output"), GH_LOG=str(temporary / "gh.log"),
                                  FAKE_EXISTING=str(existing), TAG_PREFIX="connectors/acme/python/",
                                  RELEASE_VERSION="v0.1.0", PYPI_STATUS=pypi_status,
                                  GITHUB_REPOSITORY="superdurable/dex-connectors-library"))

    def test_stale_publish_rerun_never_modifies_a_completed_release(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            result = self.run_guard(directory, built_source_sha="f" * 40, release_assets=[
                "dex_connector_acme-0.1.0-py3-none-any.whl", "language-release.complete"])
            self.assertEqual(0, result.returncode, result.stderr)
            self.assertIn("release_state=complete\n", (directory / "output").read_text())
            self.assertFalse((directory / "gh.log").exists())
        publish_job = self.job("publish-language")
        for step_name in ("Create or repair language release", "Mark language release complete"):
            step = publish_job.split(f"      - name: {step_name}\n", 1)[1].split("      - name:", 1)[0]
            self.assertIn("steps.guard.outputs.release_state != 'complete'", step.splitlines()[0])

    def test_stale_build_artifact_cannot_repair_an_incomplete_release(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            result = self.run_guard(Path(temporary), built_source_sha="f" * 40,
                                    release_assets=["dex_connector_acme-0.1.0-py3-none-any.whl"])
            self.assertNotEqual(0, result.returncode)
            self.assertIn("stale publish job", result.stderr)

    def test_github_first_repair_keeps_existing_archive_bytes(self) -> None:
        for existing_archive, should_pass in ((b"rebuilt wheel", True), (b"earlier wheel", False)):
            with self.subTest(should_pass=should_pass), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                result = self.run_guard(directory, built_source_sha=None, existing_archive=existing_archive,
                                        release_assets=["dex_connector_acme-0.1.0-py3-none-any.whl"])
                self.assertEqual(should_pass, result.returncode == 0, result.stderr)
                self.assertIn("release download", (directory / "gh.log").read_text())
                if should_pass:
                    self.assertIn("release_state=incomplete\n", (directory / "output").read_text())
                else:
                    self.assertIn("different dex_connector_acme-0.1.0-py3-none-any.whl", result.stderr)

    def test_registry_backed_repair_replaces_incomplete_release_archives(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            result = self.run_guard(directory, built_source_sha=None, existing_archive=b"earlier wheel",
                                    pypi_status="published",
                                    release_assets=["dex_connector_acme-0.1.0-py3-none-any.whl"])
            self.assertEqual(0, result.returncode, result.stderr)
            self.assertIn("release_state=incomplete\n", (directory / "output").read_text())
            self.assertFalse((directory / "gh.log").exists())


if __name__ == "__main__":
    unittest.main()
