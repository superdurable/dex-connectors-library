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
from dex_connectors.sdk.connectormock import Case

import dex_connectors.harvest as package
from dex_connectors.harvest import (
    LIST_PROJECTS_DEFINITION,
    LIST_TIME_ENTRIES_DEFINITION,
    ProjectPage,
    TimeEntryPage,
)
from dex_connectors.harvest.harvestmock import (
    Mock,
    list_projects_default_answer,
    list_projects_invalid_key,
    list_time_entries_default_answer,
    list_time_entries_invalid_key,
)
from examples.harvest_lookup.flow import FLOW_TYPE, HarvestLookupFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def wire(value: Any) -> dict[str, Any]:
    if isinstance(value, ProjectPage):
        return {
            "projects": [
                {"id": project.id, "name": project.name, "isActive": project.is_active}
                for project in value.projects
            ]
        }
    assert isinstance(value, TimeEntryPage)
    return {
        "timeEntries": [
            {
                "id": entry.id,
                "project": {"id": entry.project.id},
                "hours": entry.hours,
                "spentDate": entry.spent_date,
            }
            for entry in value.time_entries
        ]
    }


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + project["project"]["version"]
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    cases: dict[str, dict[str, Case[Any]]] = {
        "listProjects": {
            "defaultAnswer": list_projects_default_answer(),
            "invalidKey": list_projects_invalid_key(),
        },
        "listTimeEntries": {
            "defaultAnswer": list_time_entries_default_answer(),
            "invalidKey": list_time_entries_invalid_key(),
        },
    }
    for definition in (LIST_PROJECTS_DEFINITION, LIST_TIME_ENTRIES_DEFINITION):
        operation = operations[definition.operation.operation_id]
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        assert operation["execution"] == {"executeMethodTimeout": "45s", "durability": "async"}
        assert definition.step_defaults.execute_method_timeout == timedelta(seconds=45)
        assert definition.step_defaults.execute_retry == default_connector_step_execute_retry_policy()
        for mock in operation["mocks"]:
            case = cases[operation["name"]][mock["name"]]
            assert case.branch == mock["branch"]
            if "output" in mock:
                assert wire(case.value) == mock["output"]
            if "failure" in mock:
                assert case.failure is not None
                assert {"kind": case.failure.kind.value, "message": case.failure.message} == mock["failure"]


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
    flow = HarvestLookupFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
