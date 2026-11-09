# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Keep the CI summary dependent on every required workflow job and Dex test servers fast."""

import re
import unittest
from pathlib import Path


WORKFLOW_PATH = Path(__file__).resolve().parents[1] / ".github/workflows/ci.yml"
ADVISORY_JOBS = {"workspace-connector-dependents"}
WORKFLOW_DIRECTORY = WORKFLOW_PATH.parent


class CISummaryDependenciesTest(unittest.TestCase):
    def test_summary_needs_every_non_advisory_job(self) -> None:
        workflow = WORKFLOW_PATH.read_text(encoding="utf-8")
        jobs_section = workflow.split("\njobs:\n", maxsplit=1)[1]
        job_headers = list(re.finditer(r"(?m)^  ([a-z][a-z0-9-]*):\s*$", jobs_section))
        job_names = {header.group(1) for header in job_headers}
        self.assertIn("ci-summary", job_names)
        summary_header = next(header for header in job_headers if header.group(1) == "ci-summary")
        summary_end = next((header.start() for header in job_headers if header.start() > summary_header.start()), len(jobs_section))
        summary_job = jobs_section[summary_header.end():summary_end]
        needs_match = re.search(r"(?m)^    needs:\n((?:^      - [a-z][a-z0-9-]*\n)+)", summary_job)
        self.assertIsNotNone(needs_match)
        summary_needs = set(re.findall(r"(?m)^      - ([a-z][a-z0-9-]*)$", needs_match.group(1)))
        self.assertEqual(job_names - ADVISORY_JOBS - {"ci-summary"}, summary_needs - ADVISORY_JOBS)
        self.assertFalse(summary_needs - job_names)


class DexDevelopmentServerDurabilityTest(unittest.TestCase):
    def test_every_workflow_dexcli_dev_uses_process_crash_safe_durability(self) -> None:
        # dexcli 1.35 defaults to strict fsync durability, which slows test servers several times.
        launch_count = 0
        for workflow_path in sorted(WORKFLOW_DIRECTORY.glob("*.yml")):
            for launch_line in re.findall(r"(?m)^.*dexcli dev .*$", workflow_path.read_text(encoding="utf-8")):
                launch_count += 1
                self.assertIn("-durability process-crash-safe", launch_line, f"{workflow_path.name}: {launch_line.strip()}")
        self.assertGreater(launch_count, 0)


class NativeWireDatabaseServiceTest(unittest.TestCase):
    def test_mongodb_service_waits_for_ping_and_exports_its_uri(self) -> None:
        workflow = WORKFLOW_PATH.read_text(encoding="utf-8")
        self.assertEqual(2, workflow.count("mongosh --quiet --eval 'db.runCommand({ping:1}).ok'"))
        self.assertIn("export MONGODB_CONNECTOR_TEST_URI=mongodb://127.0.0.1:27017", workflow)


class GoReleaseLimitTest(unittest.TestCase):
    def test_plan_caps_go_releases_before_any_job_reads_the_matrix(self) -> None:
        workflow = (WORKFLOW_DIRECTORY / "release-connector.yml").read_text(encoding="utf-8")
        self.assertIn("      go_release_limit:\n", workflow.split("\npermissions:\n", 1)[0])
        plan_step = workflow.split("      - name: Build pending or targeted repair matrix\n", 1)[1].split("\n      - ", 1)[0]
        self.assertIn("GO_RELEASE_LIMIT: ${{ inputs.go_release_limit || vars.GO_RELEASE_LIMIT || '15' }}", plan_step)
        self.assertIn('--go-release-limit "${GO_RELEASE_LIMIT}"', plan_step)
        # Only the capped selection may write the Go matrix that release and build-language read.
        self.assertIn('arguments=(--catalog catalog.yaml --github-output "${RUNNER_TEMP}/release-matrix-output")', plan_step)
        self.assertIn('--github-output "${GITHUB_OUTPUT}"', plan_step.split("language_publish.py select", 1)[1])
        self.assertIn("go_deferred_count=0", plan_step)
        self.assertIn("matrix: ${{ steps.matrix.outputs.matrix }}", workflow)
        self.assertIn("GO_MATRIX: ${{ needs.plan.outputs.matrix }}", workflow)


if __name__ == "__main__":
    unittest.main()
