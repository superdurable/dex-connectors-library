# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fail when the hand-written Python contract drifts from connector.yaml."""

import tomllib
from datetime import timedelta
from pathlib import Path

import dex
import yaml
from dex_connectors.sdk import default_connector_step_execute_retry_policy

from dex_connectors.sentry import (
    GET_ISSUE_DEFINITION,
    LIST_ISSUES_DEFINITION,
    UPDATE_ISSUE_STATUS_DEFINITION,
    Issue,
)
from dex_connectors.sentry.sentrymock import (
    Mock,
    get_issue_default,
    list_issues_default,
    update_issue_status_default,
)
from examples.issue_review.flow import FLOW_TYPE, IssueReviewFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def issue_wire(issue: Issue) -> dict[str, str]:
    return {"id": issue.id, "shortId": issue.short_id, "title": issue.title, "status": issue.status}


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    package = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + package["project"]["version"]
    assert package["project"]["name"] == "dex-connector-sentry"
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    assert set(operations) == {"listIssues", "getIssue", "updateIssueStatus"}
    for definition, kind in (
        (LIST_ISSUES_DEFINITION, "query"),
        (GET_ISSUE_DEFINITION, "query"),
        (UPDATE_ISSUE_STATUS_DEFINITION, "mutation"),
    ):
        operation = operations[definition.operation.operation_id]
        assert operation["kind"] == kind
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        assert operation["execution"] == {"executeMethodTimeout": "30s", "durability": "async"}
        assert definition.step_defaults.execute_method_timeout == timedelta(seconds=30)
        assert definition.step_defaults.execute_durability == 2
        assert definition.step_defaults.execute_retry == default_connector_step_execute_retry_policy()
    page = list_issues_default().value
    assert page is not None
    assert operations["listIssues"]["mocks"][0]["output"] == {
        "issues": [issue_wire(issue) for issue in page.issues]
    }
    found = get_issue_default().value
    assert found is not None and operations["getIssue"]["mocks"][0]["output"] == issue_wire(found)
    updated = update_issue_status_default().value
    assert updated is not None and operations["updateIssueStatus"]["mocks"][0]["output"] == issue_wire(
        updated
    )


def test_example_registers_with_mock_connection() -> None:
    flow = IssueReviewFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
