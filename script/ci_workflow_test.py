# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Keep the CI summary dependent on every required workflow job and Dex test servers fast."""

import os
import re
import subprocess
import sys
import tempfile
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
        # Push, schedule, drain, and repair runs all see tagged Go versions whose release is incomplete.
        self.assertIn('arguments+=(--release-inventory "${GH_RELEASE_INVENTORY_PATH}")', plan_step)
        self.assertLess(plan_step.index("--release-inventory"), plan_step.index('if [ "${EVENT_NAME}" = workflow_dispatch ] && [ -n'))
        self.assertIn('--github-output "${GITHUB_OUTPUT}"', plan_step.split("language_publish.py select", 1)[1])
        self.assertIn("go_deferred_count=0", plan_step)
        self.assertIn("matrix: ${{ steps.matrix.outputs.matrix }}", workflow)
        self.assertIn("GO_MATRIX: ${{ needs.plan.outputs.matrix }}", workflow)


def workflow_job(workflow: str, job_name: str) -> str:
    """Returns the text of one top-level job of a workflow."""
    jobs_section = workflow.split("\njobs:\n", maxsplit=1)[1]
    headers = list(re.finditer(r"(?m)^  ([a-z][a-z0-9-]*):\s*$", jobs_section))
    header = next(header for header in headers if header.group(1) == job_name)
    end = next((other.start() for other in headers if other.start() > header.start()), len(jobs_section))
    return jobs_section[header.end():end]


def workflow_steps(job: str) -> list[str]:
    return re.split(r"(?m)^      - ", job.split("\n    steps:\n", 1)[1])[1:]


def step_script(job: str, step_name: str) -> str:
    """Returns the shell of one named step's run block."""
    step = next(step for step in workflow_steps(job) if step.startswith(f"name: {step_name}\n"))
    lines = step.split("        run: |\n", 1)[1].splitlines()
    return "\n".join(line[10:] for line in lines if line.startswith("          ") or not line.strip()) + "\n"


GIT_FIXTURE_ENVIRONMENT = {
    "GIT_AUTHOR_NAME": "Fixture", "GIT_AUTHOR_EMAIL": "fixture@example.com", "GIT_COMMITTER_NAME": "Fixture",
    "GIT_COMMITTER_EMAIL": "fixture@example.com", "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1",
}


class ConnectorsMCPWorkflowTest(unittest.TestCase):
    release_workflow = (WORKFLOW_DIRECTORY / "release-connector.yml").read_text(encoding="utf-8")

    def test_release_runs_after_every_go_release_without_gating_the_catalog(self) -> None:
        job = workflow_job(self.release_workflow, "release-connectors-mcp")
        self.assertIn("\n    needs: [plan, release]\n", job)
        self.assertIn("\n    if: always() && needs.plan.result == 'success'\n", job)
        self.assertIn("go-version: '1.25'", job)
        catalog = workflow_job(self.release_workflow, "build-catalog")
        catalog_needs = re.search(r"(?m)^    needs: \[(.*)\]$", catalog).group(1).split(", ")
        self.assertIn("release-connectors-mcp", catalog_needs, "the index lists a release made in this run")
        catalog_condition = re.search(r"(?m)^    if: (.*)$", catalog).group(1)
        self.assertNotIn("release-connectors-mcp", catalog_condition, "a failed server release never blocks the catalog")
        for job_name in ("build-language", "publish-language", "deploy-catalog"):
            self.assertNotIn("release-connectors-mcp", workflow_job(self.release_workflow, job_name))

    def test_release_tags_an_off_main_commit_and_marks_completion_last(self) -> None:
        job = workflow_job(self.release_workflow, "release-connectors-mcp")
        steps = workflow_steps(job)
        names = [re.match(r"(?:name: )?(.*)", step).group(1) for step in steps]
        commit_step = steps[names.index("Create the release commit off main and tag it")]
        self.assertIn('git tag "${RELEASE_TAG}"', commit_step)
        self.assertNotIn("git push", commit_step)
        publish_step = steps[names.index("Push the release tag and create or repair the GitHub release")]
        self.assertIn('push origin "refs/tags/${RELEASE_TAG}"', publish_step)
        push_lines = [line.strip() for line in job.splitlines() if re.search(r"\bpush\b", line) and "git" not in line.split("push")[1]]
        self.assertEqual(['push origin "refs/tags/${RELEASE_TAG}"'], push_lines, "only the tag is pushed, never a branch")
        self.assertEqual(2, publish_step.count("--latest=false"))
        self.assertIn("--verify-tag", publish_step)
        self.assertEqual("Mark the Connectors MCP release complete", names[-1])
        self.assertIn("connectors-mcp-release.complete", steps[-1])
        for step in steps:
            if re.search(r"gh release (create|edit|upload)|push origin", step):
                self.assertIn("inputs.dry_run != true", step, step.splitlines()[0])
        self.assertLess(names.index("Verify the published server module"), names.index("Mark the Connectors MCP release complete"))

    def run_release_commit_step(self, change) -> tuple[subprocess.CompletedProcess, Path]:
        script = step_script(workflow_job(self.release_workflow, "release-connectors-mcp"),
                             "Create the release commit off main and tag it")
        directory = Path(tempfile.mkdtemp())
        environment = {**os.environ, **GIT_FIXTURE_ENVIRONMENT, "RELEASE_TAG": "connectorsmcp/v0.1.4", "RELEASE_VERSION": "0.1.4"}
        registry = directory / "connectorsmcp/cmd/dex-connectors-mcp/zz_generated_registry.go"
        registry.parent.mkdir(parents=True)
        registry.write_text("package main\n")
        for name in ("go.mod", "go.sum", "README.md"):
            (directory / "connectorsmcp" / name).write_text(name + "\n")
        for arguments in (["init", "-q", "-b", "main"], ["add", "-A"], ["commit", "-q", "-m", "main"]):
            subprocess.run(["git", *arguments], cwd=directory, env=environment, check=True)
        (directory / "main-commit").write_text(subprocess.run(["git", "rev-parse", "HEAD"], cwd=directory, env=environment,
                                                              capture_output=True, text=True, check=True).stdout)
        change(directory)
        result = subprocess.run(["bash", "-e", "-c", script], cwd=directory, env=environment, capture_output=True, text=True)
        return result, directory

    def test_release_commit_succeeds_when_the_generated_files_equal_main(self) -> None:
        result, directory = self.run_release_commit_step(lambda _directory: None)
        self.assertEqual(0, result.returncode, result.stderr)
        parent = subprocess.run(["git", "rev-parse", "connectorsmcp/v0.1.4^1"], cwd=directory,
                                capture_output=True, text=True, check=True).stdout
        self.assertEqual((directory / "main-commit").read_text(), parent, "the release commit's parent is main")

    def test_release_commit_rejects_files_outside_the_registry_and_go_mod(self) -> None:
        result, _ = self.run_release_commit_step(lambda directory: (directory / "connectorsmcp/README.md").write_text("changed\n"))
        self.assertNotEqual(0, result.returncode)
        self.assertIn("changed files outside the registry and go.mod", result.stderr)
        accepted, _ = self.run_release_commit_step(lambda directory: (directory / "connectorsmcp/go.mod").write_text("changed\n"))
        self.assertEqual(0, accepted.returncode, accepted.stderr)

    def run_index_step(self, deployed_index: str | None) -> tuple[subprocess.CompletedProcess, Path]:
        script = step_script(workflow_job(self.release_workflow, "build-catalog"), "Generate the Connectors MCP release index")
        directory = Path(tempfile.mkdtemp())
        stubs = directory / "bin"
        stubs.mkdir()
        # The index rebuild fails; python3 -c still validates the deployed index.
        (stubs / "python3").write_text(f'#!/bin/sh\nif [ "$1" = -c ]; then exec {sys.executable} "$@"; fi\nexit 1\n')
        curl = "#!/bin/sh\nexit 22\n" if deployed_index is None else (
            '#!/bin/sh\nwhile [ "$1" != --output ]; do shift; done\n'
            f"cat > \"$2\" <<'JSON'\n{deployed_index}\nJSON\n")
        (stubs / "curl").write_text(curl)
        for stub in stubs.iterdir():
            stub.chmod(0o755)
        environment = {**os.environ, "PATH": f"{stubs}{os.pathsep}{os.environ['PATH']}"}
        result = subprocess.run(["bash", "-e", "-c", script], cwd=directory, env=environment, capture_output=True, text=True)
        return result, directory / "dist/pages/connectors-mcp/index.json"

    def test_failed_index_rebuild_never_blocks_the_catalog(self) -> None:
        result, index = self.run_index_step(None)
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertIn("this catalog deploy omits it", result.stdout)
        self.assertFalse(index.exists())
        deployed = '{"schemaVersion": "dex-connectors-mcp/release-index/v1", "releases": []}'
        result, index = self.run_index_step(deployed)
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(deployed + "\n", index.read_text())
        result, index = self.run_index_step('{"schemaVersion": "other"}')
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertFalse(index.exists(), "an unrecognized deployed index is not republished")

    def test_catalog_publishes_the_release_index_with_the_site(self) -> None:
        catalog = workflow_job(self.release_workflow, "build-catalog")
        index_step = catalog.index("connectors_mcp_release.py index --output \"${index}\"")
        self.assertIn("index=dist/pages/connectors-mcp/index.json", catalog)
        self.assertLess(index_step, catalog.index("uses: actions/upload-pages-artifact"))
        self.assertIn("https://connectors.superdurable.io/connectors-mcp/index.json", catalog)

    def test_ci_tests_the_server_with_go_1_25_and_reports_it(self) -> None:
        workflow = WORKFLOW_PATH.read_text(encoding="utf-8")
        job = workflow_job(workflow, "connectors-mcp")
        self.assertIn("if: needs.connector-plan.outputs.connectors_mcp == 'true'", job)
        self.assertIn("go-version: '1.25'", job)
        self.assertIn("make test-connectors-mcp", job)
        self.assertIn("connectors_mcp: ${{ steps.plan.outputs.connectors_mcp }}", workflow_job(workflow, "connector-plan"))
        summary = workflow_job(workflow, "ci-summary")
        self.assertIn("RUN_CONNECTORS_MCP: ${{ needs.connector-plan.outputs.connectors_mcp }}", summary)
        self.assertIn("'RUN_CONNECTORS_MCP': ('connectors-mcp',),", summary)


if __name__ == "__main__":
    unittest.main()
