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
    return {"tag_prefix": f"connectors/{name}/{language}/", "version": "v0.1.0",
            "package": f"dex-connector-{name}", "language": language}


def names(entries: list[dict[str, str]]) -> list[str]:
    return [entry["package"].removeprefix("dex-connector-").removeprefix("@superdurable/connector-")
            for entry in entries]


def tag(name: str, language: str = "python") -> str:
    return f"connectors/{name}/{language}/v0.1.0"


def never_on_pypi(_package: str, _version: str) -> bool:
    return False


class SelectReleaseMatricesTests(unittest.TestCase):
    def test_python_first_releases_go_to_github_first_and_keep_updates(self) -> None:
        items = [item("existing"), item("new-one"), item("npm-new", "typescript"), item("new-two"),
                 item("new-three"), item("completed")]
        selection = language_publish.select_release_matrices(
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
        selection = language_publish.select_release_matrices(
            items, 3, completed_tags={tag("existing-project"), tag("new-project"), tag("on-pypi"),
                                      tag("npm-done", "typescript")},
            project_exists=lambda _language, package: package != "dex-connector-new-project",
            pypi_release_exists=lambda package, _version: package == "dex-connector-on-pypi")
        self.assertEqual(["existing-project", "new-project"], names(selection["pypi_pending"]))
        self.assertNotIn("first_publish", selection["pypi_pending"][0])
        self.assertTrue(selection["pypi_pending"][1]["first_publish"])
        self.assertTrue(all(entry["github_released"] for entry in selection["pypi_pending"]))
        self.assertEqual([], selection["updates"] + selection["first"] + selection["github_first"])
        self.assertNotIn("artifact_index", selection["pypi_pending"][0])
        self.assertEqual(2, selection["pending_count"])

    def test_pending_pypi_uploads_take_project_slots_before_new_releases(self) -> None:
        items = [item("new-release"), item("pending-one"), item("pending-two"), item("pending-three")]
        selection = language_publish.select_release_matrices(
            items, 2, completed_tags={tag("pending-one"), tag("pending-two"), tag("pending-three")},
            project_exists=lambda _language, _package: False, pypi_release_exists=never_on_pypi)
        self.assertEqual(["pending-one", "pending-two"], names(selection["pypi_pending"]))
        self.assertEqual(["new-release"], names(selection["github_first"]))
        self.assertEqual(["pending-three"], names(selection["deferred"]))
        self.assertEqual("first_publish_cap", selection["deferred"][0]["defer_reason"])
        self.assertTrue(selection["deferred"][0]["github_released"])

    def test_pypi_lookup_failure_for_completed_release_retries_next_run(self) -> None:
        def pypi_release_exists(_package, _version):
            raise RuntimeError("PyPI unavailable")

        selection = language_publish.select_release_matrices(
            [item("completed")], 3, completed_tags={tag("completed")},
            project_exists=lambda _language, _package: False, pypi_release_exists=pypi_release_exists)
        self.assertEqual(0, selection["pending_count"])

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
        selection = language_publish.select_release_matrices(
            [item("new"), item("pending")], 3, completed_tags={tag("pending")},
            project_exists=lambda _language, _package: False, pypi_release_exists=never_on_pypi)
        with tempfile.TemporaryDirectory() as temporary:
            output_path = Path(temporary) / "output"
            language_publish.write_selection_outputs(output_path, selection, '{"include":[]}', "0")
            outputs = dict(line.split("=", 1) for line in output_path.read_text(encoding="utf-8").splitlines())
        self.assertEqual("1", outputs["github_first_count"])
        self.assertEqual("1", outputs["pypi_pending_count"])
        self.assertEqual("1", outputs["language_count"])
        self.assertEqual(["dex-connector-new"],
                         [entry["package"] for entry in json.loads(outputs["language_matrix"])["include"]])
        self.assertEqual(["dex-connector-pending"],
                         [entry["package"] for entry in json.loads(outputs["pypi_pending_matrix"])["include"]])


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

    def write_release(self, directory: Path) -> None:
        digests = {}
        for name, contents in ((self.wheel, b"wheel bytes"), (self.sdist, b"sdist bytes")):
            (directory / name).write_bytes(contents)
            digests[name] = hashlib.sha256(contents).hexdigest()
            (directory / f"{name}.sha256").write_text(f"{digests[name]}  {name}\n", encoding="utf-8")
        manifest = json.dumps({"language": "python", "package": self.package, "version": "v0.1.0",
                               "tag": self.release_tag, "sourceSha": "abc", "artifacts": digests}).encode("utf-8")
        (directory / "language-release.json").write_bytes(manifest)
        (directory / "language-release.json.sha256").write_text(
            f"{hashlib.sha256(manifest).hexdigest()}  language-release.json\n", encoding="utf-8")

    def verify(self, directory: Path, version: str = "v0.1.0") -> list[Path]:
        return language_publish.verify_release_archives(directory, self.release_tag, self.package, version)

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
            self.assertIn("if: steps.state.outputs.release_required == 'true'\n", step)
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
        )
        for environment, expected in cases:
            with self.subTest(environment=environment), tempfile.TemporaryDirectory() as temporary:
                subprocess.run(("bash", "-e", "-o", "pipefail", "-c", script), check=True, env=dict(
                    os.environ, RUNNER_TEMP=temporary, JOB_STATUS="success", RELEASE_REQUIRED="true",
                    TAG_PREFIX="connectors/acme/python/", RELEASE_VERSION="v0.1.0", **environment))
                result = json.loads((Path(temporary) / "release-result" / "release-result.json").read_text())
                self.assertEqual(expected, (result["status"], result["reason"]))


if __name__ == "__main__":
    unittest.main()
