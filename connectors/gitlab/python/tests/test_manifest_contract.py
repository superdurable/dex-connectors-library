# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fail when the hand-written Python contract drifts from connector.yaml."""

import inspect
import json
import tomllib
from datetime import timedelta
from pathlib import Path

import dex
import yaml
from dex_connectors.sdk import default_connector_step_execute_retry_policy

import dex_connectors.gitlab as package
from dex_connectors.gitlab import (
    GET_PROJECT_DEFINITION,
    LIST_ISSUES_DEFINITION,
    UPDATE_ISSUE_LABELS_DEFINITION,
    Issue,
)
from dex_connectors.gitlab.gitlabmock import (
    Mock,
    get_project_default_answer,
    list_issues_default_answer,
    update_issue_labels_default_answer,
)
from examples.gitlab_lookup.flow import FLOW_TYPE, GitLabLookupFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def _issue_json(issue: Issue) -> dict[str, object]:
    return {
        "id": issue.id,
        "iid": issue.iid,
        "title": issue.title,
        "labels": list(issue.labels),
        "web_url": issue.web_url,
    }


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + project["project"]["version"]
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    for definition, kind in (
        (GET_PROJECT_DEFINITION, "query"),
        (LIST_ISSUES_DEFINITION, "query"),
        (UPDATE_ISSUE_LABELS_DEFINITION, "mutation"),
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
    found = get_project_default_answer().value
    issues = list_issues_default_answer().value
    updated = update_issue_labels_default_answer().value
    assert found is not None and issues is not None and updated is not None
    assert operations["getProject"]["mocks"][0]["output"] == {
        "id": found.id,
        "name": found.name,
        "path_with_namespace": found.path_with_namespace,
        "web_url": found.web_url,
    }
    assert operations["listIssues"]["mocks"][0]["output"] == {
        "issues": [_issue_json(issue) for issue in issues.issues]
    }
    assert operations["updateIssueLabels"]["mocks"][0]["output"] == _issue_json(updated)


def test_factory_parameters_match_declared_branch_fields() -> None:
    document = json.loads((Path(package.__file__).parent / "dex-connector.json").read_text())
    common = {
        "step_type",
        "step_input_type",
        "annotations",
        "connection",
        "connection_name",
        "map_to_operation_input",
        "result_attribute",
        "step_options_override",
    }
    for factory in document["factories"]:
        parameters = set(inspect.signature(getattr(package, factory["name"])).parameters)
        assert parameters - common == {branch["field"] for branch in factory["branches"]}


def test_example_registers_with_mock_connection() -> None:
    flow = GitLabLookupFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
