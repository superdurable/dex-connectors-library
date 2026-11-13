# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Decide the single required ci-summary check from the CI plan, the event, and every job's result.

A job the plan selects must succeed. A job the plan does not select may be skipped, but never
fail or be cancelled. Queue-only jobs need a Dex stack, service containers, or Docker; pull
requests skip them by design and the merge queue, pushes to main, and manual runs require them.
"""

import json
import os
import sys
from collections.abc import Mapping

# Every job ci-summary needs; JOB_RESULTS must name exactly these.
CI_JOBS = frozenset({
    "common-python-scripts",
    "common-repository-checks",
    "common-root-go",
    "common-sdkgo",
    "common-typescript-conformance",
    "connector-conformance",
    "connector-plan",
    "connectors",
    "connectors-mcp",
    "database-language-integration",
    "database-language-plan",
    "dex-compat-canary-issue",
    "dex-compat-current",
    "dex-compat-latest-go-sdk",
    "dex-compat-released",
    "dex-integration-connectors",
    "dex-integration-sdk",
    "dex-version-plan",
    "email-language-integration",
    "email-language-plan",
    "language-connectors",
    "python-connectors-latest-sdk",
    "react",
    "sdkpython",
    "sdkpython-integration",
    "sdkts",
    "sdkts-integration",
    "site",
    "workspace-connector-dependents",
})

# GitHub Actions job results; any other value fails the check.
JOB_RESULT_VALUES = frozenset({"success", "failure", "cancelled", "skipped"})

# Jobs whose failure only warns; a dependency pull request cannot also update its dependents.
ADVISORY_JOBS = frozenset({"workspace-connector-dependents"})

# Jobs the workflow skips on pull_request; the CI workflow tests keep this list and ci.yml equal.
QUEUE_ONLY_JOBS = frozenset({
    "database-language-integration",
    "dex-compat-current",
    "dex-compat-latest-go-sdk",
    "dex-integration-connectors",
    "dex-integration-sdk",
    "email-language-integration",
    "sdkpython-integration",
    "sdkts-integration",
})

# Plan outputs that select one job each; scheduled runs skip the jobs in SCHEDULE_SKIPPED_JOBS.
PLAN_OUTPUT_JOBS = {
    "common_sdkgo": "common-sdkgo",
    "common_root_go": "common-root-go",
    "common_python_scripts": "common-python-scripts",
    "common_typescript_conformance": "common-typescript-conformance",
    "sdkpython": "sdkpython",
    "sdkts": "sdkts",
    "sdkgo_integration": "dex-integration-sdk",
    "react": "react",
    "site": "site",
    "connectors_mcp": "connectors-mcp",
}

SCHEDULE_SKIPPED_JOBS = frozenset({
    "dex-integration-connectors",
    "dex-integration-sdk",
    "dex-compat-current",
    "react",
    "sdkpython-integration",
    "sdkts-integration",
    "site",
})


def select_expected_jobs(event_name: str, plan: Mapping[str, str]) -> set[str]:
    """Returns the jobs that must succeed for this event and plan, before queue-only exclusion."""
    expected = {"connector-plan", "database-language-plan", "email-language-plan", "common-repository-checks"}
    selected = {output for output in PLAN_OUTPUT_JOBS if plan.get(output) == "true"}
    connector_count = int(plan.get("count") or "0")
    expected.update(PLAN_OUTPUT_JOBS[output] for output in selected)
    if "sdkpython" in selected:
        expected.add("sdkpython-integration")
    if "sdkts" in selected:
        expected.add("sdkts-integration")
    if connector_count or "sdkpython" in selected:
        expected.add("python-connectors-latest-sdk")
    if connector_count or selected & {"sdkpython", "sdkts", "sdkgo_integration", "connectors_mcp"}:
        expected.add("dex-version-plan")
    if connector_count:
        expected.update(("language-connectors", "connector-conformance", "connectors", "dex-compat-latest-go-sdk",
                         "dex-integration-connectors", "dex-compat-current"))
        if event_name in ("schedule", "workflow_dispatch"):
            expected.add("dex-compat-released")
    if int(plan.get("database_language_count") or "0"):
        expected.add("database-language-integration")
    if int(plan.get("email_language_count") or "0"):
        expected.add("email-language-integration")
    if event_name == "schedule":
        expected -= SCHEDULE_SKIPPED_JOBS
        expected.add("dex-compat-canary-issue")
    return expected


def find_failed_jobs(results: Mapping[str, str], event_name: str, plan: Mapping[str, str]) -> dict[str, str]:
    """Returns each job that breaks the check, mapped to the reason."""
    failed = {name: f"{result}, which is not a known job result" for name, result in results.items()
              if result not in JOB_RESULT_VALUES}
    failed.update({name: "missing from JOB_RESULTS" for name in CI_JOBS - results.keys()})
    failed.update({name: "not a job ci-summary knows" for name in results.keys() - CI_JOBS})
    if failed:
        return failed
    failed = {name: result for name, result in results.items()
              if name not in ADVISORY_JOBS and result in ("failure", "cancelled")}
    expected = select_expected_jobs(event_name, plan)
    if event_name == "pull_request":
        expected -= QUEUE_ONLY_JOBS
        for name in sorted(QUEUE_ONLY_JOBS):
            if results[name] != "skipped":
                failed[name] = f"{results[name]}, but queue-only jobs must be skipped on pull_request"
    for name in sorted(expected):
        if results[name] != "success":
            failed[name] = results[name]
    return failed


def plan_from_environment(environment: Mapping[str, str]) -> dict[str, str]:
    """Reads the plan outputs that ci.yml passes as PLAN_<OUTPUT> variables."""
    prefix = "PLAN_"
    return {name[len(prefix):].lower(): value for name, value in environment.items() if name.startswith(prefix)}


def main() -> int:
    results = {name: job["result"] for name, job in json.loads(os.environ["JOB_RESULTS"]).items()}
    event_name = os.environ["EVENT_NAME"]
    failed = find_failed_jobs(results, event_name, plan_from_environment(os.environ))
    if failed:
        for name, reason in sorted(failed.items()):
            print(f"{name}: {reason}", file=sys.stderr)
        return 1
    if event_name == "pull_request":
        print("Queue-only jobs skipped on pull_request by design; the merge queue runs them: "
              + ", ".join(sorted(QUEUE_ONLY_JOBS)))
    print("All selected CI jobs passed; unselected jobs were skipped.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
