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

import dex_connectors.close as package
from dex_connectors.close import GET_LEAD_DEFINITION, LIST_LEADS_DEFINITION, Lead, LeadPage
from dex_connectors.close.closemock import (
    Mock,
    get_lead_default_answer,
    get_lead_invalid_key,
    list_leads_default_answer,
    list_leads_invalid_key,
)
from examples.close_lookup.flow import FLOW_TYPE, CloseLookupFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def wire(value: Any) -> Any:
    if isinstance(value, LeadPage):
        return {"leads": [wire(lead) for lead in value.leads], "hasMore": value.has_more}
    assert isinstance(value, Lead)
    return {"id": value.id, "name": value.name}


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + project["project"]["version"]
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    cases: dict[str, dict[str, Case[Any]]] = {
        "listLeads": {"defaultAnswer": list_leads_default_answer(), "invalidKey": list_leads_invalid_key()},
        "getLead": {"defaultAnswer": get_lead_default_answer(), "invalidKey": get_lead_invalid_key()},
    }
    for definition in (LIST_LEADS_DEFINITION, GET_LEAD_DEFINITION):
        operation = operations[definition.operation.operation_id]
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        assert operation["execution"] == {"executeMethodTimeout": "45s", "durability": "async"}
        assert definition.step_defaults.execute_method_timeout == timedelta(seconds=45)
        assert definition.step_defaults.execute_durability == 2
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
    flow = CloseLookupFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
