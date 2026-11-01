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

import dex_connectors.vercel as package
from dex_connectors.vercel import GET_DEPLOYMENT_DEFINITION, LIST_DEPLOYMENTS_DEFINITION
from dex_connectors.vercel.vercelmock import (
    Mock,
    get_deployment_found_default,
    list_deployments_listed_default,
)
from examples.vercel_lookup.flow import FLOW_TYPE, VercelLookupFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + project["project"]["version"]
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    for definition in (LIST_DEPLOYMENTS_DEFINITION, GET_DEPLOYMENT_DEFINITION):
        operation = operations[definition.operation.operation_id]
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        assert operation["execution"] == {"executeMethodTimeout": "30s", "durability": "async"}
        assert definition.step_defaults.execute_method_timeout == timedelta(seconds=30)
        assert definition.step_defaults.execute_retry == default_connector_step_execute_retry_policy()
    page = list_deployments_listed_default().value
    deployment = get_deployment_found_default().value
    assert page is not None and deployment is not None

    def wire(item: Any) -> dict[str, Any]:
        return {
            "uid": item.uid,
            "name": item.name,
            "url": item.url,
            "state": item.state,
            "created": item.created,
        }

    assert operations["listDeployments"]["mocks"][0]["output"] == {
        "deployments": [wire(item) for item in page.deployments],
        "pagination": {"next": page.pagination.next},
    }
    assert operations["getDeployment"]["mocks"][0]["output"] == wire(deployment)


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
    flow = VercelLookupFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
