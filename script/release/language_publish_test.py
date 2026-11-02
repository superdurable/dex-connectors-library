# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Tests for language release selection and PyPI project-creation deferral."""

import json
import os
import subprocess
import tempfile
import unittest
import urllib.error
from pathlib import Path
from unittest.mock import patch

from script.release import language_publish


def item(name: str, language: str = "python") -> dict[str, str]:
    return {"tag_prefix": f"connectors/{name}/{language}/", "version": "v0.1.0",
            "package": f"dex-connector-{name}", "language": language}


class SelectReleaseMatricesTests(unittest.TestCase):
    def test_caps_only_new_python_projects_and_keeps_updates(self) -> None:
        items = [item("existing"), item("new-one"), item("new-two"), item("npm-new", "typescript"),
                 item("new-three"), item("completed")]
        selection = language_publish.select_release_matrices(
            items, 2, completed_tags={"connectors/completed/python/v0.1.0"},
            project_exists=lambda _language, package: package == "dex-connector-existing")
        self.assertEqual(["existing"], [entry["package"].removeprefix("dex-connector-") for entry in selection["updates"]])
        self.assertEqual(["new-one", "new-two", "npm-new"],
                         [entry["package"].removeprefix("dex-connector-") for entry in selection["first"]])
        self.assertEqual(["new-three"],
                         [entry["package"].removeprefix("dex-connector-") for entry in selection["deferred"]])
        self.assertEqual("first_publish_cap", selection["deferred"][0]["defer_reason"])
        self.assertEqual([0, 1, 2, 3], [entry["artifact_index"] for entry in selection["updates"] + selection["first"]])
        self.assertEqual(5, selection["pending_count"])

    def test_rejects_zero_limit(self) -> None:
        with self.assertRaisesRegex(ValueError, "positive"):
            language_publish.select_release_matrices([], 0)

    def test_lookup_failure_defers_one_language_without_stopping_others(self) -> None:
        def project_exists(_language, package):
            if package == "dex-connector-unavailable":
                raise RuntimeError("registry unavailable")
            return False

        selection = language_publish.select_release_matrices(
            [item("unavailable"), item("available")], 1, project_exists=project_exists)
        self.assertEqual(["dex-connector-available"], [entry["package"] for entry in selection["first"]])
        self.assertEqual(["dex-connector-unavailable"], [entry["package"] for entry in selection["deferred"]])
        self.assertEqual("registry_lookup_failure", selection["deferred"][0]["defer_reason"])

    def test_caps_after_filtering_more_than_256_declared_languages(self) -> None:
        selection = language_publish.select_release_matrices(
            [item(f"project-{index}") for index in range(300)], 3,
            project_exists=lambda _language, _package: False)
        self.assertEqual(3, len(selection["first"]))
        self.assertEqual(297, len(selection["deferred"]))

    def test_defers_after_selected_matrix_reaches_256(self) -> None:
        selection = language_publish.select_release_matrices(
            [item(f"existing-{index}") for index in range(300)], 3,
            project_exists=lambda _language, _package: True)
        self.assertEqual(256, len(selection["updates"]))
        self.assertEqual(44, len(selection["deferred"]))


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
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            matrix_output = directory / "matrix"
            github_output = directory / "output"
            matrix_output.write_text("matrix={\"include\":[{\"directory\":\"connectors/acme\"}]}\ncount=1\n"
                                     + "language_matrix=" + json.dumps({"include": [item("acme")]}) + "\n", encoding="utf-8")
            arguments = ["language_publish.py", "select", "--matrix-output", str(matrix_output),
                         "--github-output", str(github_output), "--first-publish-limit", "3"]
            with patch.object(language_publish, "list_completed_language_tags", side_effect=RuntimeError("rate limited")), \
                 patch.object(language_publish.sys, "argv", arguments), \
                 patch.dict(os.environ, {"LANGUAGE_SELECTION_PATH": str(directory / "selection.json")}):
                self.assertEqual(0, language_publish.main())
            output = github_output.read_text(encoding="utf-8")
            self.assertIn("count=1\n", output)
            self.assertIn("deferred_count=1\n", output)
            selection = json.loads((directory / "selection.json").read_text(encoding="utf-8"))
            self.assertEqual("release_inventory_unavailable", selection["deferred"][0]["defer_reason"])

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


if __name__ == "__main__":
    unittest.main()
