# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Tests for language release selection and PyPI project-creation deferral."""

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

from script.release import language_publish


def item(name: str, language: str = "python") -> dict[str, str]:
    return {"tag_prefix": f"connectors/{name}/{language}/", "version": "v0.1.0",
            "package": f"dex-connector-{name}", "language": language}


class SelectReleaseMatricesTests(unittest.TestCase):
    def test_caps_only_new_python_projects_and_keeps_updates(self) -> None:
        items = [item("existing"), item("new-one"), item("new-two"), item("npm-new", "typescript"),
                 item("new-three"), item("completed")]
        selection = language_publish.select_release_matrices(
            items, 2, is_complete=lambda entry: entry["package"] == "dex-connector-completed",
            project_exists=lambda _language, package: package == "dex-connector-existing")
        self.assertEqual(["existing"], [entry["package"].removeprefix("dex-connector-") for entry in selection["updates"]])
        self.assertEqual(["new-one", "new-two", "npm-new"],
                         [entry["package"].removeprefix("dex-connector-") for entry in selection["first"]])
        self.assertEqual(["new-three"],
                         [entry["package"].removeprefix("dex-connector-") for entry in selection["deferred"]])
        self.assertEqual([0, 1, 2, 3], [entry["artifact_index"] for entry in selection["updates"] + selection["first"]])
        self.assertEqual(5, selection["pending_count"])

    def test_rejects_zero_limit(self) -> None:
        with self.assertRaisesRegex(ValueError, "positive"):
            language_publish.select_release_matrices([], 0)


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
            summary = language_publish.summarize(
                [item("published"), item("missing")], directory, [item("capped")])
        self.assertEqual(["connectors/published/python/v0.1.0"], summary["published"])
        self.assertEqual(["connectors/capped/python/v0.1.0"], summary["deferred"])
        self.assertEqual(["connectors/missing/python/v0.1.0"], summary["failed"])
        self.assertEqual(2, summary["pending_count"])


if __name__ == "__main__":
    unittest.main()
