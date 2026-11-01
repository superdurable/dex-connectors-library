# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fail when the hand-written Python contract drifts from connector.yaml."""

import inspect
import json
import tomllib
from datetime import timedelta
from pathlib import Path
from typing import Any

import dex
import yaml
from dex_connectors.sdk import default_connector_step_execute_retry_policy

import dex_connectors.atlassian_bitbucket as package
from dex_connectors.atlassian_bitbucket import (
    LIST_PULL_REQUESTS_DEFINITION,
    LIST_REPOSITORIES_DEFINITION,
    PullRequestPage,
    RepositoryPage,
)
from dex_connectors.atlassian_bitbucket.bitbucketmock import (
    Mock,
    list_pull_requests_default_answer,
    list_repositories_default_answer,
)
from examples.resource_read.flow import FLOW_TYPE, ResourceReadFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def page_wire(page: RepositoryPage | PullRequestPage) -> dict[str, Any]:
    if isinstance(page, RepositoryPage):
        values: list[dict[str, Any]] = [
            {"uuid": item.uuid, "name": item.name, "full_name": item.full_name, "is_private": item.is_private}
            for item in page.values
        ]
    else:
        values = [{"id": item.id, "title": item.title, "state": item.state} for item in page.values]
    return {"values": values, "page": page.page, "pagelen": page.pagelen}


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + project["project"]["version"]
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    for definition in (LIST_REPOSITORIES_DEFINITION, LIST_PULL_REQUESTS_DEFINITION):
        operation = operations[definition.operation.operation_id]
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        assert operation["execution"] == {"executeMethodTimeout": "30s", "durability": "async"}
        assert definition.step_defaults.execute_method_timeout == timedelta(seconds=30)
        assert definition.step_defaults.execute_retry == default_connector_step_execute_retry_policy()
    repositories = list_repositories_default_answer().value
    pulls = list_pull_requests_default_answer().value
    assert repositories is not None and pulls is not None
    assert operations["listRepositories"]["mocks"][0]["output"] == page_wire(repositories)
    assert operations["listPullRequests"]["mocks"][0]["output"] == page_wire(pulls)


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
    flow = ResourceReadFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
