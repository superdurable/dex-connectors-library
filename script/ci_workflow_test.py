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


class MergeQueueTriggerTest(unittest.TestCase):
    def test_merge_groups_plan_changed_connectors_like_pull_requests(self) -> None:
        workflow = WORKFLOW_PATH.read_text(encoding="utf-8")
        triggers = workflow.split("\njobs:\n", maxsplit=1)[0]
        self.assertRegex(triggers, r"(?m)^  merge_group:\s*$")
        plan = workflow_job(workflow, "connector-plan")
        self.assertRegex(plan, r"CONNECTOR_SCOPE: \$\{\{.*github.event_name == 'merge_group'.*\}\}")
        self.assertRegex(plan, r"CONNECTOR_BASE: \$\{\{.*github.event.merge_group.base_sha.*\}\}")
        scopes = re.findall(r"CONNECTOR_SCOPE: \$\{\{(.*)\}\}", workflow)
        self.assertGreater(len(scopes), 1)
        for scope in scopes:
            self.assertTrue("github.event_name == 'merge_group'" in scope or "needs.connector-plan.outputs.connector_scope" in scope, scope)
        bases = re.findall(r"(?:CONNECTOR_BASE|BASE_SHA): \$\{\{(.*)\}\}", workflow)
        self.assertTrue(bases)
        for base in bases:
            self.assertTrue("github.event.merge_group.base_sha" in base or base.strip() == "needs.connector-plan.outputs.connector_base", base)

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
        health = workflow_job(self.release_workflow, "check-connectors-mcp-index")
        self.assertIn("\n    needs: [build-catalog, deploy-catalog]\n", health)
        self.assertNotIn("check-connectors-mcp-index", workflow_job(self.release_workflow, "deploy-catalog"))

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

    def run_index_step(self, deployed_index: str | None, rebuild: str = "exit 1") -> tuple[subprocess.CompletedProcess, Path, str]:
        script = step_script(workflow_job(self.release_workflow, "build-catalog"), "Generate the Connectors MCP release index")
        directory = Path(tempfile.mkdtemp())
        stubs = directory / "bin"
        stubs.mkdir()
        # python3 -c still validates the deployed index; rebuild stands in for the index command.
        (stubs / "python3").write_text(f'#!/bin/sh\nif [ "$1" = -c ]; then exec {sys.executable} "$@"; fi\n{rebuild}\n')
        curl = "#!/bin/sh\nexit 22\n" if deployed_index is None else (
            '#!/bin/sh\nwhile [ "$1" != --output ]; do shift; done\n'
            f"cat > \"$2\" <<'JSON'\n{deployed_index}\nJSON\n")
        (stubs / "curl").write_text(curl)
        for stub in stubs.iterdir():
            stub.chmod(0o755)
        (directory / "temp").mkdir()
        environment = {**os.environ, "PATH": f"{stubs}{os.pathsep}{os.environ['PATH']}",
                       "RUNNER_TEMP": str(directory / "temp"), "GITHUB_OUTPUT": str(directory / "output")}
        result = subprocess.run(["bash", "-e", "-c", script], cwd=directory, env=environment, capture_output=True, text=True)
        outputs = (directory / "output").read_text() if (directory / "output").exists() else ""
        return result, directory / "dist/pages/connectors-mcp/index.json", outputs

    def run_health_step(self, outcome: str, dropped: str = "", unverified: str = "") -> subprocess.CompletedProcess:
        script = step_script(workflow_job(self.release_workflow, "check-connectors-mcp-index"),
                             "Fail when the Connectors MCP index fell back, was omitted, or dropped a version")
        return subprocess.run(["bash", "-e", "-c", script], env={**os.environ, "INDEX_OUTCOME": outcome, "DROPPED_TAGS": dropped,
                                                                   "UNVERIFIED_TAGS": unverified},
                              capture_output=True, text=True)

    def test_failed_index_rebuild_never_blocks_the_catalog_but_turns_the_health_check_red(self) -> None:
        result, index, outputs = self.run_index_step(None)
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertIn("this catalog deploy omits it", result.stdout)
        self.assertFalse(index.exists())
        self.assertEqual("outcome=omitted\n", outputs)
        deployed = '{"schemaVersion": "dex-connectors-mcp/release-index/v1", "releases": []}'
        result, index, outputs = self.run_index_step(deployed)
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(deployed + "\n", index.read_text())
        self.assertEqual("outcome=fell_back\n", outputs)
        health = self.run_health_step("fell_back")
        self.assertNotEqual(0, health.returncode, "a newest-release failure is red after the catalog deploys")
        self.assertIn("deployed index was republished", health.stdout)
        result, index, outputs = self.run_index_step('{"schemaVersion": "other"}')
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertFalse(index.exists(), "an unrecognized deployed index is not republished")

    def test_index_health_check_reports_each_outcome(self) -> None:
        self.assertEqual(0, self.run_health_step("ok").returncode)
        for outcome in ("omitted", "dropped_versions", "reused_unverified", "empty", ""):
            with self.subTest(outcome):
                self.assertNotEqual(0, self.run_health_step(outcome, "connectorsmcp/v0.1.1").returncode)
        self.assertIn("left out connectorsmcp/v0.1.1", self.run_health_step("dropped_versions", "connectorsmcp/v0.1.1").stdout)
        self.assertIn("could not verify: connectorsmcp/v0.1.2",
                      self.run_health_step("reused_unverified", unverified="connectorsmcp/v0.1.2").stdout)

    def test_successful_rebuild_passes_the_deployed_index_and_records_its_outcome(self) -> None:
        deployed = '{"schemaVersion": "dex-connectors-mcp/release-index/v1", "releases": []}'
        rebuild = ('for argument; do case "$previous" in --deployed-index) cp "$argument" "$OLDPWD/seen";; '
                   '--output) echo "{}" > "$argument";; --github-output) echo outcome=ok >> "$argument";; esac; '
                   'previous="$argument"; done')
        result, index, outputs = self.run_index_step(deployed, rebuild=f'OLDPWD="$PWD"; {rebuild}')
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual("outcome=ok\n", outputs)
        self.assertEqual("{}\n", index.read_text())
        self.assertEqual(deployed + "\n", (index.parents[3] / "seen").read_text(), "older entries can come from the deployed index")

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
        from script import ci_summary

        self.assertIn("PLAN_CONNECTORS_MCP: ${{ needs.connector-plan.outputs.connectors_mcp }}", summary)
        self.assertEqual("connectors-mcp", ci_summary.PLAN_OUTPUT_JOBS["connectors_mcp"])


MAKEFILE_PATH = WORKFLOW_PATH.parents[2] / "Makefile"
GO_DOWNLOAD_RETRY_COMMAND = "$(GO_DOWNLOAD_RETRY)"


def makefile_target(makefile: str, target: str) -> tuple[str, str]:
    """Returns one Makefile target's prerequisite line and recipe."""
    match = re.search(rf"(?m)^{re.escape(target)}:(.*)\n((?:\t.*\n)*)", makefile)
    if match is None:
        raise AssertionError(f"Makefile target {target} is missing")
    return match.group(1), match.group(2)


class GoDownloadRetryTest(unittest.TestCase):
    """Network downloads finish under bounded retries before any job runs Go tests."""

    makefile = MAKEFILE_PATH.read_text(encoding="utf-8")
    workflow = WORKFLOW_PATH.read_text(encoding="utf-8")

    def test_ci_jobs_run_the_targets_that_download_before_testing(self) -> None:
        for job_name, target in (("connectors", "test-connectors"), ("dex-integration-connectors", "test-connector-integration"),
                                 ("dex-compat-current", "test-dex-compat-current")):
            with self.subTest(job=job_name):
                self.assertRegex(workflow_job(self.workflow, job_name), rf"make [^\n]*\b{target}\b")
        # Each language shard runs test-python-connectors or test-typescript-connectors.
        self.assertIn("- run: make test-${{ matrix.language }}-connectors\n", workflow_job(self.workflow, "language-connectors"))

    def test_connector_go_modules_download_before_their_tests(self) -> None:
        self.assertIn("GO_DOWNLOAD_RETRY = python3 script/go_download_retry.py\n", self.makefile)
        # The prewarm loads the same packages and build tags as the test that follows it.
        for target, download_command, test_command in (
                ("test-connectors", f'{GO_DOWNLOAD_RETRY_COMMAND} "$$module/go" || exit 1;', "go test -race ./..."),
                ("test-connector-integration", f'{GO_DOWNLOAD_RETRY_COMMAND} --tags integration "$$module/go" || exit 1;',
                 "go test -p 1 -tags=integration ./...")):
            with self.subTest(target=target):
                _, recipe = makefile_target(self.makefile, target)
                download = recipe.find(download_command)
                self.assertGreaterEqual(download, 0)
                self.assertLess(download, recipe.index(test_command))

    def test_root_module_downloads_before_connectorctl_runs(self) -> None:
        _, recipe = makefile_target(self.makefile, "download-root-go-modules")
        self.assertEqual(f"\t@{GO_DOWNLOAD_RETRY_COMMAND} --without-tests --package ./cmd/connectorctl .\n", recipe)
        for target in ("test-connectors", "test-python-connectors", "test-python-connectors-latest-sdk", "test-typescript-connectors", "test-connector-integration",
                       "test-dex-compat-current", "test-dex-compat-released", "test-dex-compat-latest-go-sdk"):
            with self.subTest(target=target):
                prerequisites, _ = makefile_target(self.makefile, target)
                self.assertIn("download-root-go-modules", prerequisites.split())

    def test_dex_web_downloads_its_toolchain_and_modules_before_go_test(self) -> None:
        source = (WORKFLOW_PATH.parents[2] / "script/dex_compatibility.py").read_text(encoding="utf-8")
        dex_web = source.split("\ndef test_dex_web(", 1)[1].split("\ndef ", 1)[0]
        prewarm = dex_web.index('run_go_network_command(go_package_prewarm_command(["."], "", True), web, environment')
        self.assertLess(prewarm, dex_web.index('run(["go", "test", ".",'))

    def test_retry_helper_tests_run_in_common_ci(self) -> None:
        _, recipe = makefile_target(self.makefile, "test-common-python-scripts")
        self.assertIn("script/go_download_retry_test.py", recipe)


COMMON_BLOCKS = {
    # job: (plan output, Makefile target)
    "common-sdkgo": ("common_sdkgo", "test-common-sdkgo"),
    "common-root-go": ("common_root_go", "test-common-root-go"),
    "common-python-scripts": ("common_python_scripts", "test-common-python-scripts"),
    "common-typescript-conformance": ("common_typescript_conformance", "test-common-typescript-conformance"),
}


class CommonBlockJobsTest(unittest.TestCase):
    """Each common block is its own path-selected job, and make test-common still runs them all."""

    workflow = WORKFLOW_PATH.read_text(encoding="utf-8")
    makefile = MAKEFILE_PATH.read_text(encoding="utf-8")

    def test_each_block_runs_only_its_target_when_the_plan_selects_it(self) -> None:
        plan = workflow_job(self.workflow, "connector-plan")
        self.assertNotRegex(self.workflow, r"(?m)^  common:\s*$")
        self.assertNotIn("outputs.common ", self.workflow)
        self.assertNotIn("outputs.common }}", self.workflow)
        for job_name, (output, target) in COMMON_BLOCKS.items():
            with self.subTest(job=job_name):
                job = workflow_job(self.workflow, job_name)
                self.assertIn(f"{output}: ${{{{ steps.plan.outputs.{output} }}}}", plan)
                self.assertIn(f"\n    needs: connector-plan\n    if: needs.connector-plan.outputs.{output} == 'true'\n", job)
                self.assertEqual([f"make {target}"], re.findall(r"(?m)^\s+- run: (make .*)$", job))
                self.assertNotIn("github.event_name", job.split("\n    steps:\n", 1)[0], "blocks run on every event")

    def test_make_test_common_runs_every_block(self) -> None:
        prerequisites, recipe = makefile_target(self.makefile, "test-common")
        self.assertEqual(sorted([*(target for _, target in COMMON_BLOCKS.values()), "test-common-repository-checks"]),
                         sorted(prerequisites.split()))
        self.assertEqual("", recipe)

    def test_repository_checks_run_on_every_event_and_change(self) -> None:
        job = workflow_job(self.workflow, "common-repository-checks")
        header = job.split("\n    steps:\n", 1)[0]
        self.assertNotIn("if:", header)
        self.assertNotIn("needs:", header, "the checks start without waiting for the plan")
        self.assertEqual(["make test-common-repository-checks"], re.findall(r"(?m)^\s+- run: (make .*)$", job))
        _, recipe = makefile_target(self.makefile, "test-common-repository-checks")
        self.assertIn("go test -race $(ROOT_GO_REPOSITORY_CHECK_PACKAGES)", recipe)
        self.assertIn("go test -count=1 -run '$(ROOT_GO_REPOSITORY_CHECK_TESTS)' ./cmd/connectorctl", recipe)
        _, root = makefile_target(self.makefile, "test-common-root-go")
        self.assertIn('grep -vxF "$$repository_checks"', root, "the root suite does not rerun the repository checks")

    def test_each_job_sets_up_only_the_toolchains_its_block_needs(self) -> None:
        uses = {job_name: set(re.findall(r"uses: ([a-z-]+/[a-z-]+(?:/[a-z]+)?)@", workflow_job(self.workflow, job_name)))
                for job_name in COMMON_BLOCKS}
        self.assertEqual({"actions/checkout", "actions/setup-go", "actions/cache/restore", "actions/cache/save"}, uses["common-sdkgo"])
        self.assertEqual({"actions/checkout", "actions/setup-go", "actions/cache/restore", "actions/cache/save"},
                         set(re.findall(r"uses: ([a-z-]+/[a-z-]+(?:/[a-z]+)?)@", workflow_job(self.workflow, "common-repository-checks"))))
        self.assertEqual({"actions/checkout", "actions/setup-node", "actions/setup-go", "actions/cache/restore", "actions/cache/save"},
                         uses["common-root-go"])
        self.assertEqual({"actions/checkout", "actions/setup-go", "actions/setup-node"}, uses["common-python-scripts"])
        self.assertEqual({"actions/checkout", "actions/setup-node"}, uses["common-typescript-conformance"])
        # The root suite compiles generated TypeScript with both packages' node_modules.
        _, recipe = makefile_target(self.makefile, "test-common-root-go")
        self.assertIn("npm ci --prefix connectors/stripe/typescript\n", recipe)
        self.assertIn("npm ci --prefix connectors/google/gmail/typescript\n", recipe)
        self.assertLess(recipe.index("npm ci"), recipe.index("go test"))

    def test_go_blocks_restore_main_caches_and_only_main_saves_them(self) -> None:
        for job_name in ("common-repository-checks", "common-sdkgo", "common-root-go"):
            with self.subTest(job=job_name):
                job = workflow_job(self.workflow, job_name)
                self.assertIn("          cache: false\n", job, "setup-go saves only on a key miss, so main never refreshes it")
                self.assertLess(job.index("run: python3 script/set_mtimes_from_git_objects.py"), job.index("make test-common"))
                restore = job.split("uses: actions/cache/restore@v6\n", 1)[1].split("\n      - ", 1)[0]
                key = re.search(r"key: (.*)", restore).group(1)
                self.assertTrue(key.startswith(f"{job_name}-${{{{ runner.os }}}}-go${{{{ steps.setup-go.outputs.go-version }}}}-"), key)
                self.assertTrue(key.endswith("-${{ github.sha }}"), key)
                self.assertIn("restore-keys: |", restore)
                for path in ("~/.cache/go-build", "~/go/pkg/mod"):
                    self.assertIn(path, restore)
                save = job.split("uses: actions/cache/save@v6\n", 1)[0].rsplit("\n      - ", 1)[1]
                self.assertIn("if: github.ref == 'refs/heads/main' && steps.go-cache.outputs.cache-hit != 'true'", save)
                self.assertLess(job.index("make test-common"), job.index("actions/cache/save"))
                clean = job.index("run: go clean -testcache")
                self.assertIn("if: github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'\n        run: go clean -testcache", job)
                self.assertLess(job.index("uses: actions/cache/restore@v6"), clean)
                self.assertLess(clean, job.index("make test-common"))

    def test_only_tests_with_untracked_inputs_disable_the_go_test_cache(self) -> None:
        _, sdkgo = makefile_target(self.makefile, "test-common-sdkgo")
        self.assertIn("SDKGO_UNCACHED_TESTS = ^TestWrongFactoryTargetInputDoesNotCompile$$\n", self.makefile)
        self.assertIn("\tcd sdkgo && GOWORK=off go test -race -skip '$(SDKGO_UNCACHED_TESTS)' ./...\n", sdkgo)
        self.assertIn("\tcd sdkgo && GOWORK=off go test -race -count=1 -run '$(SDKGO_UNCACHED_TESTS)' .\n", sdkgo)
        _, root = makefile_target(self.makefile, "test-common-root-go")
        self.assertIn("ROOT_GO_CACHEABLE_TEST_PACKAGES = ./internal/codegen/gomodel\n", self.makefile)
        # Packages default to -count=1; a new package is cached only after review adds it to the list.
        self.assertIn('grep -vxF "$$cacheable"', root)
        self.assertIn("go test -race -count=1 -timeout=30m $$uncached", root)
        self.assertIn("go test -race $$cacheable", root)

    def test_root_go_recipe_fails_when_either_test_run_fails(self) -> None:
        _, recipe = makefile_target(self.makefile, "test-common-root-go")
        recipe = recipe.replace("npm ci", "true").replace("\tGOWORK=off go vet ./...\n", "")
        directory = Path(tempfile.mkdtemp())
        stubs = directory / "bin"
        stubs.mkdir()
        # The go stub lists packages, then fails the run named in FAILING_RUN.
        (stubs / "go").write_text('#!/bin/sh\nif [ "$1 $2" = "list ./..." ]; then printf "./cmd/x\\n./internal/compliance\\n"; exit 0; fi\n'
                                  'if [ "$1" = list ]; then shift; for p in "$@"; do echo "$p"; done; exit 0; fi\n'
                                  'case " $* " in *" -count=1 "*) run=uncached;; *) run=cacheable;; esac\n'
                                  'echo "$run $*" >> "$LOG"; [ "$run" != "$FAILING_RUN" ]\n')
        (stubs / "go").chmod(0o755)
        (directory / "Makefile").write_text("ROOT_GO_CACHEABLE_TEST_PACKAGES = ./internal/compliance\nrun:\n" + recipe)
        for failing_run in ("", "uncached", "cacheable"):
            with self.subTest(failing_run=failing_run or "none"):
                log = directory / f"log-{failing_run}"
                result = subprocess.run(["make", "-s", "run"], cwd=directory, capture_output=True, text=True,
                                        env={**os.environ, "PATH": f"{stubs}{os.pathsep}{os.environ['PATH']}",
                                             "FAILING_RUN": failing_run, "LOG": str(log)})
                self.assertEqual(failing_run == "", result.returncode == 0, result.stderr)
                runs = sorted(log.read_text().splitlines())
                self.assertEqual(["cacheable test -race ./internal/compliance", "uncached test -race -count=1 -timeout=30m ./cmd/x"], runs)


class QueueOnlyJobsTest(unittest.TestCase):
    """Pull requests run fast checks; the merge queue also runs jobs that need a Dex stack or services."""

    workflow = WORKFLOW_PATH.read_text(encoding="utf-8")
    pull_request_exclusion = "github.event_name != 'pull_request'"

    def job_condition(self, job_name: str) -> str:
        """Returns the job's condition without the explicit dependency status checks of trusted-skip jobs."""
        match = re.search(r"(?m)^    if: (.*)$", workflow_job(self.workflow, job_name).split("\n    steps:\n", 1)[0])
        return strip_dependency_status_checks(match.group(1)) if match else ""

    def test_workflow_skips_exactly_the_summary_queue_only_jobs_on_pull_requests(self) -> None:
        from script import ci_summary

        jobs_section = self.workflow.split("\njobs:\n", 1)[1]
        job_names = re.findall(r"(?m)^  ([a-z][a-z0-9-]*):\s*$", jobs_section)
        skipped = {name for name in job_names if self.pull_request_exclusion in self.job_condition(name)}
        self.assertEqual(set(ci_summary.QUEUE_ONLY_JOBS), skipped)
        for name in skipped:
            self.assertTrue(self.job_condition(name).startswith(("github.event_name != 'schedule' && " + self.pull_request_exclusion,
                                                                 self.pull_request_exclusion)), name)

    def test_long_running_and_fast_jobs_stay_on_pull_requests(self) -> None:
        from script import ci_summary

        for name in ("language-connectors", "connectors", "python-connectors-latest-sdk", "workspace-connector-dependents",
                     "connectors-mcp", "sdkpython", "sdkts", "react", "site", "dex-version-plan", "common-repository-checks",
                     "connector-conformance",
                     *COMMON_BLOCKS):
            with self.subTest(job=name):
                self.assertNotIn(name, ci_summary.QUEUE_ONLY_JOBS)
                self.assertNotIn("pull_request", self.job_condition(name))

    def step_conditions(self, job_name: str) -> dict[str, str]:
        conditions = {}
        for step in workflow_steps(workflow_job(self.workflow, job_name)):
            command = re.search(r"(?m)^\s*(?:- )?run: (.*)$", step)
            condition = re.search(r"(?m)^\s*if: (.*)$", step)
            if command:
                conditions[command.group(1)] = condition.group(1) if condition else ""
        return conditions

    def test_package_tests_and_conformance_replay_run_in_parallel_jobs(self) -> None:
        # Package tests took about 61 minutes (run 43271436573) and every-fixture replay about 39
        # (run 43339498254); in one job they would approach the 120-minute merge queue timeout.
        rust_toolchain = 'rustup toolchain install "${RUSTUP_TOOLCHAIN}" --profile minimal && rustup default "${RUSTUP_TOOLCHAIN}"'
        self.assertEqual({
            rust_toolchain: "matrix.language == 'rust'",
            "make test-${{ matrix.language }}-connectors": "",
        }, self.step_conditions("language-connectors"))
        for job_name in ("language-connectors", "connector-conformance"):
            header = workflow_job(self.workflow, job_name).split("\n    steps:\n", 1)[0]
            self.assertIn("\n    needs: [connector-plan, local-verification]\n"
                          "    if: \"!cancelled() && needs.connector-plan.result == 'success' && needs.connector-plan.outputs.count != '0'"
                          " && needs.local-verification.outputs.trusted != 'true'\"\n", header)
        self.assertNotIn("all_connectors", self.workflow)

    def test_every_event_replays_the_selected_connectors_and_only_pull_requests_and_canaries_mutate(self) -> None:
        conditions = self.step_conditions("connector-conformance")
        rust_toolchain = 'rustup toolchain install "${RUSTUP_TOOLCHAIN}" --profile minimal && rustup default "${RUSTUP_TOOLCHAIN}"'
        self.assertEqual({rust_toolchain, "make test-connector-conformance",
                          "python3 script/connector_conformance.py --mutation-self-test"}, set(conditions))
        self.assertEqual("", conditions[rust_toolchain], "Rust packages replay on every event")
        self.assertEqual("", conditions["make test-connector-conformance"], "the queue replays every selected fixture")
        mutation = conditions["python3 script/connector_conformance.py --mutation-self-test"]
        for event_name, runs in (("pull_request", True), ("merge_group", False), ("push", False), ("schedule", True),
                                 ("workflow_dispatch", True)):
            with self.subTest(event=event_name):
                self.assertEqual(runs, evaluate_condition(mutation, {"github.event_name": event_name}))

    def test_conformance_shards_come_from_the_plan(self) -> None:
        plan = workflow_job(self.workflow, "connector-plan")
        self.assertIn("conformance_matrix: ${{ steps.conformance.outputs.matrix }}", plan)
        self.assertIn('matrix="$(python3 script/connector_conformance.py "${arguments[@]}")"', plan)
        job = workflow_job(self.workflow, "connector-conformance")
        self.assertIn("matrix: ${{ fromJSON(needs.connector-plan.outputs.conformance_matrix) }}", job)
        self.assertIn("fail-fast: false", job)
        self.assertIn("CONFORMANCE_SHARD_INDEX: ${{ matrix.shard_index }}", job)
        self.assertIn("CONFORMANCE_SHARD_COUNT: ${{ matrix.shard_count }}", job)

    def test_replay_includes_every_plan_selected_connector(self) -> None:
        makefile = MAKEFILE_PATH.read_text(encoding="utf-8")
        prerequisites, recipe = makefile_target(makefile, "test-connector-conformance")
        self.assertIn("download-root-go-modules", prerequisites.split())
        self.assertIn('go run ./cmd/connectorctl test-matrix $(CONNECTOR_SELECTION_ARGUMENTS))" || exit 1;', recipe)
        self.assertIn('arguments="$$arguments --include-connector $$directory"', recipe)
        self.assertIn('if [ "$(CONNECTOR_SCOPE)" != all ]; then arguments="$$arguments --base $(CONNECTOR_BASE) --head $(CONNECTOR_HEAD)"; fi;', recipe)

    def test_connectors_mcp_runs_unit_tests_on_pull_requests_and_the_dex_example_in_the_queue(self) -> None:
        steps = workflow_steps(workflow_job(self.workflow, "connectors-mcp"))
        unit = next(step for step in steps if "make test-connectors-mcp\n" in step)
        self.assertNotIn("if:", unit)
        for marker in ("astral-sh/setup-uv", "install-dexcli", "make test-connectors-mcp-example"):
            step = next(step for step in steps if marker in step)
            self.assertIn(f"if: {self.pull_request_exclusion}\n", step, marker)

    def test_released_compatibility_runs_only_on_schedule_and_manual_runs(self) -> None:
        self.assertEqual("(github.event_name == 'schedule' || github.event_name == 'workflow_dispatch') && needs.connector-plan.outputs.count != '0'",
                         self.job_condition("dex-compat-released"))


class LanguageConnectorShardsTest(unittest.TestCase):
    """Language package tests run in plan-sized shards that together test each package once."""

    workflow = WORKFLOW_PATH.read_text(encoding="utf-8")
    makefile = MAKEFILE_PATH.read_text(encoding="utf-8")
    clock_test = "uv run --project connectors/airtable/python python conformance/runners/python/clock_test.py"
    slice_wire_test = "python3 script/python_slice_wire_test.py"

    def test_shards_come_from_the_plan_and_the_summary_requires_every_shard(self) -> None:
        plan = workflow_job(self.workflow, "connector-plan")
        self.assertIn("language_connector_matrix: ${{ steps.plan.outputs.language_connector_matrix }}", plan)
        job = workflow_job(self.workflow, "language-connectors")
        header = job.split("\n    steps:\n", 1)[0]
        self.assertIn("    strategy:\n      fail-fast: false\n"
                      "      matrix: ${{ fromJSON(needs.connector-plan.outputs.language_connector_matrix) }}\n", header)
        self.assertIn("CONNECTOR_SHARD_INDEX: ${{ matrix.shard_index }}", header)
        self.assertIn("CONNECTOR_SHARD_COUNT: ${{ matrix.shard_count }}", header)
        # A matrix job's result is failure when any shard fails, unless a shard may continue on error.
        self.assertNotIn("continue-on-error", job)
        self.assertIn("      - language-connectors\n", workflow_job(self.workflow, "ci-summary"))

    def test_shards_test_each_package_once_and_run_the_shared_checks_once(self) -> None:
        root, directories, packages = self.make_fixture_repository()
        for shard_count in (1, 2, 3, 7):
            with self.subTest(shard_count=shard_count):
                runs = [self.run_language_targets(root, {"CONNECTOR_SHARD_INDEX": str(index), "CONNECTOR_SHARD_COUNT": str(shard_count)})
                        for index in range(shard_count)]
                for language in ("python", "typescript"):
                    tested = [module for run in runs for module in run[language]]
                    self.assertEqual(sorted(packages[language]), sorted(tested), language)
                if shard_count > 1:
                    self.assertGreater(sum(1 for run in runs if run["typescript"]), 1, "the shards split the packages")
                self.assertEqual([1] + [0] * (shard_count - 1), [run["shared"] for run in runs])
                for index, run in enumerate(runs):
                    self.assertEqual([f"--shard-index {index} --shard-count {shard_count}"] * 2, run["matrix"])
        # Without CI's shard variables, make tests every package and the shared checks once, as before.
        local = self.run_language_targets(root, {})
        self.assertEqual((sorted(packages["python"]), sorted(packages["typescript"]), 1),
                         (sorted(local["python"]), sorted(local["typescript"]), local["shared"]))
        self.assertEqual(["--shard-index 0 --shard-count 1"] * 2, local["matrix"])
        self.assertLess(set(packages["python"]), set(directories))

    def test_a_failing_shared_check_fails_shard_zero(self) -> None:
        root, _, _ = self.make_fixture_repository()
        for failing_command in ("clock_test.py", "python_slice_wire_test.py"):
            with self.subTest(failing_command=failing_command):
                result = self.run_make(root, "test-python-connectors", {"CONNECTOR_SHARD_INDEX": "0", "CONNECTOR_SHARD_COUNT": "2",
                                                                        "STUB_FAILING_COMMAND": failing_command})
                self.assertNotEqual(0, result.returncode)

    def make_fixture_repository(self) -> tuple[Path, list[str], dict[str, list[str]]]:
        """Writes connector directories, some without a Python or TypeScript package, and the stubbed tools."""
        root = Path(tempfile.mkdtemp())
        directories = [f"connectors/company/connector-{index:02d}" for index in range(24)]
        packages: dict[str, list[str]] = {"python": [], "typescript": []}
        for index, directory in enumerate(directories):
            if index % 5 != 4:
                (root / directory / "python").mkdir(parents=True)
                (root / directory / "python/pyproject.toml").write_text("")
                (root / directory / "python/uv.lock").write_text("")
                packages["python"].append(directory)
            if index % 3 != 2:
                (root / directory / "typescript/node_modules/.bin").mkdir(parents=True)
                (root / directory / "typescript/package.json").write_text("{}")
                (root / directory / "typescript/package-lock.json").write_text("{}")
                self.write_stub(root / directory / "typescript/node_modules/.bin/tsx", "#!/bin/sh\necho \"tsx $*\" >> \"$STUB_LOG\"\n")
                packages["typescript"].append(directory)
        (root / "catalog.yaml").write_text("directories:\n" + "".join(f"  - {directory}\n" for directory in directories))
        stubs = root / "bin"
        stubs.mkdir()
        # Like connectorctl test-matrix, the go stub lists each catalog directory in one SHA-256 bucket.
        self.write_stub(stubs / "go", f"#!{sys.executable}\n" + """import hashlib, os, sys
arguments = sys.argv[1:]
with open(os.environ["STUB_LOG"], "a") as log:
    log.write("go " + " ".join(arguments) + "\\n")
if arguments[:3] != ["run", "./cmd/connectorctl", "test-matrix"]:
    sys.exit(1)
options = dict(zip(arguments[3::2], arguments[4::2]))
index, count = int(options["--shard-index"]), int(options["--shard-count"])
with open(options["--catalog"]) as catalog:
    for line in catalog:
        directory = line.strip().removeprefix("- ")
        if directory.startswith("connectors/") and int.from_bytes(hashlib.sha256(directory.encode()).digest()[:8], "big") % count == index:
            print(directory)
""")
        for tool in ("uv", "npm", "python3"):
            self.write_stub(stubs / tool, '#!/bin/sh\necho "$(basename "$0") $*" >> "$STUB_LOG"\n'
                                          'case "$*" in *"${STUB_FAILING_COMMAND:-no failing command}"*) exit 1;; esac\n')
        variables = re.search(r"(?ms)^CONNECTOR_CATALOG \?=.*?^endif\n", self.makefile)
        self.assertIsNotNone(variables)
        targets = "".join(f"{target}:\n{makefile_target(self.makefile, target)[1]}"
                          for target in ("test-python-connectors", "test-typescript-connectors"))
        (root / "Makefile").write_text(variables.group(0) + targets)
        return root, directories, packages

    @staticmethod
    def write_stub(path: Path, contents: str) -> None:
        path.write_text(contents)
        path.chmod(0o755)

    def run_make(self, root: Path, target: str, variables: dict[str, str]) -> subprocess.CompletedProcess:
        log = root / "log"
        log.write_text("")
        environment = {name: value for name, value in os.environ.items() if not name.startswith(("CONNECTOR_", "MAKE"))}
        environment.update({"PATH": f"{root / 'bin'}{os.pathsep}{os.environ['PATH']}", "STUB_LOG": str(log), **variables})
        return subprocess.run(["make", "-s", target], cwd=root, capture_output=True, text=True, env=environment)

    def run_language_targets(self, root: Path, variables: dict[str, str]) -> dict:
        """Runs both language targets for one shard; returns the packages each tested and the shared-check count."""
        run: dict = {"python": [], "typescript": [], "shared": 0, "matrix": []}
        for target in ("test-python-connectors", "test-typescript-connectors"):
            result = self.run_make(root, target, variables)
            self.assertEqual(0, result.returncode, result.stderr)
            log = (root / "log").read_text().splitlines()
            for line in log:
                if line.startswith("go run ./cmd/connectorctl test-matrix "):
                    run["matrix"].append(re.search(r"--shard-index \d+ --shard-count \d+", line).group(0))
                elif python := re.fullmatch(r"uv run --project (\S+)/python pytest \1/python/tests", line):
                    run["python"].append(python.group(1))
                elif typescript := re.fullmatch(r"npm test --prefix (\S+)/typescript", line):
                    run["typescript"].append(typescript.group(1))
            if target == "test-python-connectors":
                self.assertEqual(log.count(self.clock_test), log.count(self.slice_wire_test))
                run["shared"] = log.count(self.clock_test)
        return run


class LocalVerificationJobsTest(unittest.TestCase):
    """A trusted Mac Studio verification skips exactly ci-summary's heavy jobs; untrusted runs keep every job."""

    workflow = WORKFLOW_PATH.read_text(encoding="utf-8")
    trusted_skip = " && needs.local-verification.outputs.trusted != 'true'"

    def job_header(self, job_name: str) -> str:
        return workflow_job(self.workflow, job_name).split("\n    steps:\n", 1)[0]

    def job_condition(self, job_name: str) -> str:
        """Returns the job's full condition, unquoted."""
        match = re.search(r"(?m)^    if: (.*)$", self.job_header(job_name))
        return unquote_condition(match.group(1)) if match else ""

    def job_needs(self, job_name: str) -> list[str]:
        match = re.search(r"(?m)^    needs: (.*)$", self.job_header(job_name))
        if not match:
            return []
        return [need.strip() for need in match.group(1).strip("[]").split(",")]

    def test_decision_job_starts_at_once_with_read_only_permissions(self) -> None:
        header = self.job_header("local-verification")
        self.assertNotIn("needs:", header)
        self.assertNotIn("if:", header)
        self.assertIn("    permissions:\n      contents: read\n      pull-requests: read\n      statuses: read\n    outputs:\n", header)
        self.assertIn("trusted: ${{ steps.decide.outputs.trusted }}", header)
        job = workflow_job(self.workflow, "local-verification")
        for line in ("GITHUB_TOKEN: ${{ github.token }}", "PULL_REQUEST_HEAD_SHA: ${{ github.event.pull_request.head.sha }}",
                     "MERGE_GROUP_HEAD_REF: ${{ github.event.merge_group.head_ref }}",
                     "MERGE_GROUP_BASE_SHA: ${{ github.event.merge_group.base_sha }}",
                     "PUSH_REF: ${{ github.event_name == 'push' && github.ref || '' }}",
                     "PUSH_SHA: ${{ github.event_name == 'push' && github.sha || '' }}",
                     "PUSH_BEFORE_SHA: ${{ github.event.before }}", "run: python3 script/ci_local_verification.py"):
            self.assertIn(line, job)

    def test_workflow_skips_exactly_the_summary_heavy_jobs_when_trusted(self) -> None:
        from script import ci_summary

        jobs_section = self.workflow.split("\njobs:\n", 1)[1]
        job_names = re.findall(r"(?m)^  ([a-z][a-z0-9-]*):\s*$", jobs_section)
        gated = {name for name in job_names if "needs.local-verification.outputs.trusted != 'true'" in self.job_condition(name)}
        self.assertEqual(set(ci_summary.LOCALLY_VERIFIED_SKIPPED_JOBS), gated)
        for name in gated:
            with self.subTest(job=name):
                self.assertTrue(self.job_condition(name).endswith(self.trusted_skip), name)
                self.assertEqual("local-verification", self.job_needs(name)[-1])

    def test_jobs_connector_ci_does_not_reproduce_never_wait_for_local_verification(self) -> None:
        for name in ("workspace-connector-dependents", "dex-integration-sdk", "database-language-integration",
                     "email-language-integration", "sdkpython-integration", "sdkts-integration"):
            with self.subTest(job=name):
                self.assertNotIn("local-verification", self.job_needs(name))
                self.assertNotIn("local-verification", self.job_condition(name))
                self.assertNotIn("cancelled()", self.job_condition(name))

    def test_heavy_jobs_require_their_original_needs_explicitly_but_not_local_verification(self) -> None:
        # An implicit success() would also skip heavy jobs when local-verification fails or is cancelled.
        from script import ci_summary

        for name in ci_summary.LOCALLY_VERIFIED_SKIPPED_JOBS:
            with self.subTest(job=name):
                original_needs = self.job_needs(name)[:-1]
                self.assertTrue(original_needs)
                status_checks = "".join(f"needs.{need}.result == 'success' && " for need in original_needs)
                self.assertTrue(self.job_condition(name).startswith("!cancelled() && " + status_checks), self.job_condition(name))
                self.assertNotIn("needs.local-verification.result", self.job_condition(name))

    def test_untrusted_runs_keep_every_heavy_job_condition(self) -> None:
        from script import ci_summary

        for name in ci_summary.LOCALLY_VERIFIED_SKIPPED_JOBS:
            # The run is not cancelled; evaluate_condition knows no functions.
            condition = self.job_condition(name).replace("!cancelled()", "True")
            original = strip_dependency_status_checks(self.job_condition(name))[:-len(self.trusted_skip)]
            original_needs = self.job_needs(name)[:-1]
            contexts = set(re.findall(r"(?:github|needs)\.[a-z0-9_.-]+", original))
            for values in ({"github.event_name": "merge_group", "needs.connector-plan.outputs.count": "1"},
                           {"github.event_name": "pull_request", "needs.connector-plan.outputs.count": "0"},
                           {"github.event_name": "push", "needs.connector-plan.outputs.sdkpython": "true"}):
                filled = {context: values.get(context, "1") for context in contexts}
                succeeded = {f"needs.{need}.result": "success" for need in original_needs}
                # Failed, cancelled, or skipped local-verification leaves trusted empty; the job runs as before.
                for trusted in ("false", ""):
                    with self.subTest(job=name, values=values, trusted=trusted):
                        self.assertEqual(evaluate_condition(original, filled), evaluate_condition(
                            condition, filled | succeeded | {"needs.local-verification.outputs.trusted": trusted}))
                with self.subTest(job=name, values=values, trusted="true"):
                    self.assertFalse(evaluate_condition(
                        condition, filled | succeeded | {"needs.local-verification.outputs.trusted": "true"}))
                for need in original_needs:
                    for result in ("failure", "cancelled", "skipped"):
                        with self.subTest(job=name, values=values, need=need, result=result):
                            self.assertFalse(evaluate_condition(condition, filled | succeeded | {
                                f"needs.{need}.result": result, "needs.local-verification.outputs.trusted": ""}))

    def test_generation_checks_run_only_when_trusted_and_connectors_are_selected(self) -> None:
        self.assertEqual(["connector-plan", "local-verification"], self.job_needs("connector-generation"))
        condition = self.job_condition("connector-generation")
        for trusted, count, runs in (("true", "3", True), ("true", "0", False), ("false", "3", False), ("", "3", False)):
            with self.subTest(trusted=trusted, count=count):
                self.assertEqual(runs, evaluate_condition(condition, {"needs.local-verification.outputs.trusted": trusted,
                                                                      "needs.connector-plan.outputs.count": count}))
        self.assertEqual(["make check-connector-generation"],
                         re.findall(r"(?m)^\s+- run: (make .*)$", workflow_job(self.workflow, "connector-generation")))
        makefile = MAKEFILE_PATH.read_text(encoding="utf-8")
        prerequisites, recipe = makefile_target(makefile, "check-connector-generation")
        _, connectors_recipe = makefile_target(makefile, "test-connectors")
        self.assertIn("download-root-go-modules", prerequisites.split())
        self.assertIn('go run ./cmd/connectorctl test-matrix $(CONNECTOR_SELECTION_ARGUMENTS))" || exit 1;', recipe)
        for command in ('validate "$$module/connector.yaml"', 'generate --check "$$module/connector.yaml"'):
            self.assertIn(command, recipe)
            self.assertIn(command, connectors_recipe)


def unquote_condition(condition: str) -> str:
    """Removes the YAML double quotes a condition starting with ! needs."""
    if len(condition) >= 2 and condition[0] == condition[-1] == '"':
        return condition[1:-1]
    return condition


def strip_dependency_status_checks(condition: str) -> str:
    """Removes a leading `!cancelled() && needs.<job>.result == 'success' && ...` prefix."""
    return re.sub(r"^!cancelled\(\) && (?:needs\.[a-z0-9-]+\.result == 'success' && )*", "", unquote_condition(condition))


def evaluate_condition(condition: str, values: dict[str, str]) -> bool:
    """Evaluates a workflow `if:` built from ==, !=, &&, ||, and parentheses over the given contexts."""
    expression = condition
    for name in sorted(values, key=len, reverse=True):
        expression = expression.replace(name, repr(values[name]))
    expression = expression.replace("&&", " and ").replace("||", " or ")
    if re.search(r"[A-Za-z_]+\.[A-Za-z_]", re.sub(r"'[^']*'", "", expression)):
        raise AssertionError(f"unknown context in {condition}")
    return bool(eval(expression, {"__builtins__": {}}))  # noqa: S307 - the expression comes from ci.yml.


class ConnectorPlanBaseFallbackTest(unittest.TestCase):
    """An unknown base commit plans every connector and CI job instead of failing or skipping."""

    def run_plan_step(self, base: str, main_relation: str = "none") -> tuple[subprocess.CompletedProcess, str, str]:
        """Runs the plan step; main_relation is none, behind (main is an older commit), or head (head is on main)."""
        script = step_script(workflow_job(WORKFLOW_PATH.read_text(encoding="utf-8"), "connector-plan"), "Plan affected Connector shards")
        directory = Path(tempfile.mkdtemp())
        stubs = directory / "bin"
        stubs.mkdir()
        (stubs / "make").write_text("#!/bin/sh\nexit 0\n")
        (stubs / "go").write_text('#!/bin/sh\necho "$@" > "$GO_ARGUMENTS"\n')
        for stub in stubs.iterdir():
            stub.chmod(0o755)
        environment = {**os.environ, **GIT_FIXTURE_ENVIRONMENT, "PATH": f"{stubs}{os.pathsep}{os.environ['PATH']}"}
        for arguments in (["init", "-q", "-b", "main"], ["commit", "-q", "--allow-empty", "-m", "base"]):
            subprocess.run(["git", *arguments], cwd=directory, env=environment, check=True)
        if main_relation == "behind":
            subprocess.run(["git", "update-ref", "refs/remotes/origin/main", "HEAD"], cwd=directory, env=environment, check=True)
            subprocess.run(["git", "commit", "-q", "--allow-empty", "-m", "feature"], cwd=directory, env=environment, check=True)
        head = subprocess.run(["git", "rev-parse", "HEAD"], cwd=directory, env=environment, capture_output=True, text=True, check=True).stdout.strip()
        if main_relation == "head":
            subprocess.run(["git", "update-ref", "refs/remotes/origin/main", "HEAD"], cwd=directory, env=environment, check=True)
        self.main_commit = subprocess.run(["git", "rev-parse", "HEAD~1" if main_relation == "behind" else "HEAD"], cwd=directory,
                                          env=environment, capture_output=True, text=True, check=True).stdout.strip()
        environment.update({"CONNECTOR_SCOPE": "changed", "CONNECTOR_BASE": base or head, "CONNECTOR_HEAD": head,
                            "GITHUB_OUTPUT": str(directory / "output"), "GO_ARGUMENTS": str(directory / "go-arguments")})
        result = subprocess.run(["bash", "-e", "-c", script], cwd=directory, env=environment, capture_output=True, text=True)
        return result, (directory / "output").read_text(), (directory / "go-arguments").read_text()

    def test_known_base_plans_changed_scope(self) -> None:
        result, outputs, go_arguments = self.run_plan_step("")
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertIn("--scope changed", go_arguments)
        self.assertIn("--base ", go_arguments)
        self.assertRegex(outputs, r"connector_scope=changed\nconnector_base=[0-9a-f]{40}\n")

    def test_unknown_base_uses_the_merge_base_with_main(self) -> None:
        result, outputs, go_arguments = self.run_plan_step("0000000000000000000000000000000000000000", main_relation="behind")
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertIn("--scope changed --head", go_arguments)
        self.assertIn(f"--base {self.main_commit}", go_arguments)
        self.assertEqual(f"connector_scope=changed\nconnector_base={self.main_commit}\n", outputs)
        self.assertIn("using the merge base with main", result.stdout)

    def test_unknown_base_on_main_plans_everything(self) -> None:
        result, outputs, go_arguments = self.run_plan_step("not-a-commit", main_relation="head")
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertIn("--scope all", go_arguments)
        self.assertEqual("connector_scope=all\nconnector_base=\n", outputs)

    def test_unknown_base_plans_everything(self) -> None:
        for base in ("0000000000000000000000000000000000000000", "not-a-commit"):
            with self.subTest(base=base):
                result, outputs, go_arguments = self.run_plan_step(base)
                self.assertEqual(0, result.returncode, result.stderr)
                self.assertIn("--scope all", go_arguments)
                self.assertNotIn("--base", go_arguments)
                self.assertEqual("connector_scope=all\nconnector_base=\n", outputs)
                self.assertIn("Unknown base commit", result.stdout)


class TestMatrixFailureTest(unittest.TestCase):
    """A failing test-matrix fails its target instead of yielding an empty list."""

    makefile = MAKEFILE_PATH.read_text(encoding="utf-8")

    def test_no_recipe_pipes_test_matrix_output(self) -> None:
        for line in self.makefile.splitlines():
            if "test-matrix" in line:
                self.assertNotIn("|", line.replace("||", ""), line)
                self.assertIn(")\" || exit 1", line)

    def test_dex_compat_targets_fail_when_test_matrix_fails(self) -> None:
        for target in ("current", "released", "latest-go-sdk"):
            _, recipe = makefile_target(self.makefile, f"test-dex-compat-{target}")
            self.assertRegex(recipe, r"test-matrix [^\n]*\)\" \|\| exit 1;")
            self.assertIn("paste -sd, -", recipe)


if __name__ == "__main__":
    unittest.main()
