# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Test that ci-summary requires selected jobs, and queue-only jobs only outside pull requests."""

import json
import os
import re
import subprocess
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from script import ci_summary  # noqa: E402
from script.ci_workflow_test import WORKFLOW_PATH, workflow_job  # noqa: E402

ALL_JOBS = sorted(ci_summary.CI_JOBS)
NOTHING_SELECTED = {output: "false" for output in ci_summary.PLAN_OUTPUT_JOBS} | {
    "count": "0", "database_language_count": "0", "email_language_count": "0"}


def results_with(successful: set[str], **overrides: str) -> dict[str, str]:
    results = {name: "success" if name in successful else "skipped" for name in ALL_JOBS}
    results.update({name.replace("_", "-"): result for name, result in overrides.items()})
    return results


PLAN_JOBS = {"connector-plan", "database-language-plan", "email-language-plan", "common-repository-checks", "local-verification"}
CONNECTOR_PLAN = NOTHING_SELECTED | {"count": "1"}
CONNECTOR_PULL_REQUEST_JOBS = PLAN_JOBS | {"dex-version-plan", "language-connectors", "connector-conformance", "connectors",
                                           "python-connectors-latest-sdk"}
CONNECTOR_QUEUE_JOBS = CONNECTOR_PULL_REQUEST_JOBS | {"dex-integration-connectors", "dex-compat-current", "dex-compat-latest-go-sdk"}


class CISummaryTest(unittest.TestCase):
    def test_site_only_pull_request_passes_with_every_common_block_skipped(self) -> None:
        plan = NOTHING_SELECTED | {"site": "true"}
        self.assertEqual({}, ci_summary.find_failed_jobs(results_with(PLAN_JOBS | {"site"}), "pull_request", plan))

    def test_selected_common_block_must_succeed(self) -> None:
        for output, job in (("common_sdkgo", "common-sdkgo"), ("common_root_go", "common-root-go"),
                            ("common_python_scripts", "common-python-scripts"),
                            ("common_typescript_conformance", "common-typescript-conformance")):
            plan = NOTHING_SELECTED | {output: "true"}
            for event_name in ("pull_request", "merge_group", "push", "schedule", "workflow_dispatch"):
                always = PLAN_JOBS | ({"dex-compat-canary-issue"} if event_name == "schedule" else set())
                with self.subTest(job=job, event=event_name):
                    self.assertEqual({}, ci_summary.find_failed_jobs(results_with(always | {job}), event_name, plan))
                    self.assertEqual({job: "skipped"}, ci_summary.find_failed_jobs(results_with(always), event_name, plan))
                    failed = ci_summary.find_failed_jobs(results_with(always | {job}, **{job: "failure"}), event_name, plan)
                    self.assertEqual({job: "failure"}, failed)

    def test_unselected_job_may_skip_but_never_fail_or_cancel(self) -> None:
        for result in ("failure", "cancelled"):
            with self.subTest(result=result):
                failed = ci_summary.find_failed_jobs(results_with(PLAN_JOBS, **{"common-root-go": result}), "pull_request", NOTHING_SELECTED)
                self.assertEqual({"common-root-go": result}, failed)

    def test_pull_request_skips_queue_only_jobs_by_design(self) -> None:
        results = results_with(CONNECTOR_PULL_REQUEST_JOBS)
        self.assertEqual({}, ci_summary.find_failed_jobs(results, "pull_request", CONNECTOR_PLAN))

    def test_pull_request_rejects_a_queue_only_job_that_ran(self) -> None:
        results = results_with(CONNECTOR_PULL_REQUEST_JOBS | {"dex-compat-current"})
        failed = ci_summary.find_failed_jobs(results, "pull_request", CONNECTOR_PLAN)
        self.assertEqual(["dex-compat-current"], list(failed))
        self.assertIn("must be skipped on pull_request", failed["dex-compat-current"])

    def test_merge_queue_and_push_require_selected_queue_only_jobs(self) -> None:
        for event_name in ("merge_group", "push", "workflow_dispatch"):
            expected_jobs = CONNECTOR_QUEUE_JOBS | ({"dex-compat-released"} if event_name == "workflow_dispatch" else set())
            with self.subTest(event=event_name):
                self.assertEqual({}, ci_summary.find_failed_jobs(results_with(expected_jobs), event_name, CONNECTOR_PLAN))
                for job in ("dex-integration-connectors", "dex-compat-current", "dex-compat-latest-go-sdk"):
                    for result in ("skipped", "failure", "cancelled"):
                        failed = ci_summary.find_failed_jobs(results_with(expected_jobs, **{job: result}), event_name, CONNECTOR_PLAN)
                        self.assertEqual({job: result}, failed)

    def test_every_queue_only_job_is_required_by_the_merge_queue_when_selected(self) -> None:
        plan = NOTHING_SELECTED | {"count": "1", "sdkpython": "true", "sdkts": "true", "sdkgo_integration": "true",
                                   "database_language_count": "2", "email_language_count": "1"}
        queue = ci_summary.select_expected_jobs("merge_group", plan)
        self.assertLessEqual(ci_summary.QUEUE_ONLY_JOBS, queue)
        pull_request_results = results_with(queue - ci_summary.QUEUE_ONLY_JOBS)
        self.assertEqual({}, ci_summary.find_failed_jobs(pull_request_results, "pull_request", plan))
        failed = ci_summary.find_failed_jobs(pull_request_results, "merge_group", plan)
        self.assertEqual({job: "skipped" for job in ci_summary.QUEUE_ONLY_JOBS}, failed)

    def test_language_integration_jobs_are_required_when_their_plan_selects_packages(self) -> None:
        plan = NOTHING_SELECTED | {"database_language_count": "1"}
        self.assertEqual({"database-language-integration": "skipped"},
                         ci_summary.find_failed_jobs(results_with(PLAN_JOBS), "merge_group", plan))
        self.assertEqual({}, ci_summary.find_failed_jobs(results_with(PLAN_JOBS), "pull_request", plan))

    def test_schedule_keeps_its_canary_and_skips_pull_request_only_suites(self) -> None:
        plan = NOTHING_SELECTED | {"count": "1", "site": "true", "react": "true", "sdkgo_integration": "true"}
        expected = PLAN_JOBS | {"dex-version-plan", "language-connectors", "connector-conformance", "connectors", "python-connectors-latest-sdk",
                                "dex-compat-latest-go-sdk", "dex-compat-released", "dex-compat-canary-issue"}
        self.assertEqual(expected, ci_summary.select_expected_jobs("schedule", plan))

    def test_repository_checks_are_required_on_every_event(self) -> None:
        for event_name in ("pull_request", "merge_group", "push", "schedule", "workflow_dispatch"):
            with self.subTest(event=event_name):
                self.assertIn("common-repository-checks", ci_summary.select_expected_jobs(event_name, NOTHING_SELECTED))

    def test_unknown_results_fail_even_for_unselected_jobs(self) -> None:
        for result in ("timed_out", "neutral", "", "SUCCESS"):
            with self.subTest(result=result):
                failed = ci_summary.find_failed_jobs(results_with(PLAN_JOBS, **{"common-root-go": result}), "pull_request", NOTHING_SELECTED)
                self.assertEqual(["common-root-go"], list(failed))
                self.assertIn("not a known job result", failed["common-root-go"])

    def test_job_results_must_name_exactly_the_known_jobs(self) -> None:
        missing = results_with(PLAN_JOBS)
        del missing["site"]
        self.assertEqual({"site": "missing from JOB_RESULTS"}, ci_summary.find_failed_jobs(missing, "pull_request", NOTHING_SELECTED))
        extra = results_with(PLAN_JOBS) | {"new-job": "success"}
        self.assertEqual({"new-job": "not a job ci-summary knows"}, ci_summary.find_failed_jobs(extra, "pull_request", NOTHING_SELECTED))

    def test_known_jobs_match_the_workflow_summary_needs(self) -> None:
        summary = workflow_job(WORKFLOW_PATH.read_text(encoding="utf-8"), "ci-summary")
        needs = set(re.findall(r"(?m)^      - ([a-z][a-z0-9-]*)$", summary.split("\n    needs:\n", 1)[1].split("\n    runs-on:", 1)[0]))
        self.assertEqual(set(ci_summary.CI_JOBS), needs)

    def test_conformance_replay_is_required_on_every_event_that_selects_connectors(self) -> None:
        for event_name in ("pull_request", "merge_group", "push", "schedule", "workflow_dispatch"):
            with self.subTest(event=event_name):
                self.assertIn("connector-conformance", ci_summary.select_expected_jobs(event_name, CONNECTOR_PLAN))
                self.assertNotIn("connector-conformance", ci_summary.select_expected_jobs(event_name, NOTHING_SELECTED))

    def test_advisory_job_failure_never_fails_the_summary(self) -> None:
        results = results_with(CONNECTOR_PULL_REQUEST_JOBS, **{"workspace-connector-dependents": "failure"})
        self.assertEqual({}, ci_summary.find_failed_jobs(results, "pull_request", CONNECTOR_PLAN))

    def test_main_reads_the_workflow_environment(self) -> None:
        environment = {**os.environ, "EVENT_NAME": "pull_request",
                       "JOB_RESULTS": json.dumps({name: {"result": result} for name, result in results_with(PLAN_JOBS).items()})}
        environment.update({f"PLAN_{name.upper()}": value for name, value in NOTHING_SELECTED.items()})
        script = Path(ci_summary.__file__)
        passed = subprocess.run([sys.executable, str(script)], env=environment, capture_output=True, text=True, check=False)
        self.assertEqual(0, passed.returncode, passed.stderr)
        self.assertIn("Queue-only jobs skipped on pull_request by design", passed.stdout)
        environment["PLAN_COMMON_ROOT_GO"] = "true"
        failed = subprocess.run([sys.executable, str(script)], env=environment, capture_output=True, text=True, check=False)
        self.assertEqual(1, failed.returncode)
        self.assertIn("common-root-go: skipped", failed.stderr)

    def test_trusted_local_verification_lets_heavy_jobs_skip_and_requires_generation_checks(self) -> None:
        plan = NOTHING_SELECTED | {"count": "1", "sdkpython": "true", "sdkts": "true", "sdkgo_integration": "true",
                                   "database_language_count": "1", "email_language_count": "1"}
        for event_name in ("pull_request", "merge_group"):
            with self.subTest(event=event_name):
                expected = ci_summary.select_expected_jobs(event_name, plan) - ci_summary.LOCALLY_VERIFIED_SKIPPED_JOBS
                if event_name == "pull_request":
                    expected -= ci_summary.QUEUE_ONLY_JOBS
                trusted_results = results_with(expected | {"connector-generation"})
                self.assertEqual({}, ci_summary.find_failed_jobs(trusted_results, event_name, plan, True))
                self.assertEqual({"connector-generation": "skipped"},
                                 ci_summary.find_failed_jobs(results_with(expected), event_name, plan, True))
                for result in ("failure", "cancelled"):
                    failed = ci_summary.find_failed_jobs(results_with(expected | {"connector-generation"}, connectors=result),
                                                         event_name, plan, True)
                    self.assertEqual({"connectors": result}, failed)

    def test_trusted_merge_group_still_requires_jobs_connector_ci_does_not_reproduce(self) -> None:
        not_reproduced = {"dex-integration-sdk", "database-language-integration", "email-language-integration",
                          "sdkpython-integration", "sdkts-integration"}
        self.assertFalse((not_reproduced | {"workspace-connector-dependents"}) & ci_summary.LOCALLY_VERIFIED_SKIPPED_JOBS)
        plan = NOTHING_SELECTED | {"count": "1", "sdkpython": "true", "sdkts": "true", "sdkgo_integration": "true",
                                   "database_language_count": "1", "email_language_count": "1"}
        expected = ci_summary.select_expected_jobs("merge_group", plan) - ci_summary.LOCALLY_VERIFIED_SKIPPED_JOBS
        self.assertLessEqual(not_reproduced, expected)
        for job in sorted(not_reproduced):
            with self.subTest(job=job):
                results = results_with((expected - {job}) | {"connector-generation"})
                self.assertEqual({job: "skipped"}, ci_summary.find_failed_jobs(results, "merge_group", plan, True))

    def test_untrusted_runs_still_require_every_heavy_job(self) -> None:
        for event_name, is_locally_verified in (("merge_group", False), ("push", True), ("workflow_dispatch", True), ("schedule", True)):
            with self.subTest(event=event_name, is_locally_verified=is_locally_verified):
                expected = ci_summary.select_expected_jobs(event_name, CONNECTOR_PLAN)
                skipped_heavy = results_with((expected - ci_summary.LOCALLY_VERIFIED_SKIPPED_JOBS) | {"connector-generation"})
                failed = ci_summary.find_failed_jobs(skipped_heavy, event_name, CONNECTOR_PLAN, is_locally_verified)
                self.assertEqual({job: "skipped" for job in expected & ci_summary.LOCALLY_VERIFIED_SKIPPED_JOBS
                                  if job not in ci_summary.ADVISORY_JOBS}, failed)
                self.assertTrue(failed)

    def test_trusted_output_is_ignored_unless_local_verification_succeeded(self) -> None:
        expected = ci_summary.select_expected_jobs("merge_group", CONNECTOR_PLAN) - ci_summary.LOCALLY_VERIFIED_SKIPPED_JOBS
        for result in ("failure", "cancelled", "skipped"):
            with self.subTest(result=result):
                results = results_with(expected | {"connector-generation"}, **{"local-verification": result})
                failed = ci_summary.find_failed_jobs(results, "merge_group", CONNECTOR_PLAN, True)
                self.assertEqual(result, failed["local-verification"])
                self.assertEqual("skipped", failed["connectors"])

    def test_main_reads_the_trusted_output(self) -> None:
        expected = ci_summary.select_expected_jobs("merge_group", CONNECTOR_PLAN) - ci_summary.LOCALLY_VERIFIED_SKIPPED_JOBS
        environment = {**os.environ, "EVENT_NAME": "merge_group", "LOCAL_VERIFICATION_TRUSTED": "true",
                       "JOB_RESULTS": json.dumps({name: {"result": result}
                                                  for name, result in results_with(expected | {"connector-generation"}).items()})}
        environment.update({f"PLAN_{name.upper()}": value for name, value in CONNECTOR_PLAN.items()})
        script = Path(ci_summary.__file__)
        passed = subprocess.run([sys.executable, str(script)], env=environment, capture_output=True, text=True, check=False)
        self.assertEqual(0, passed.returncode, passed.stderr)
        self.assertIn("A Mac Studio verification trusted this run", passed.stdout)
        environment["LOCAL_VERIFICATION_TRUSTED"] = "false"
        failed = subprocess.run([sys.executable, str(script)], env=environment, capture_output=True, text=True, check=False)
        self.assertEqual(1, failed.returncode)
        self.assertIn("connectors: skipped", failed.stderr)

    def test_workflow_passes_every_plan_output_the_summary_reads(self) -> None:
        summary = workflow_job(WORKFLOW_PATH.read_text(encoding="utf-8"), "ci-summary")
        for output in NOTHING_SELECTED:
            source = "database-language-plan.outputs.count" if output == "database_language_count" else (
                "email-language-plan.outputs.count" if output == "email_language_count" else f"connector-plan.outputs.{output}")
            with self.subTest(output=output):
                self.assertIn(f"PLAN_{output.upper()}: ${{{{ needs.{source} }}}}", summary)
        self.assertIn("LOCAL_VERIFICATION_TRUSTED: ${{ needs.local-verification.outputs.trusted }}", summary)
        self.assertIn("run: python3 script/ci_summary.py", summary)


if __name__ == "__main__":
    unittest.main()
