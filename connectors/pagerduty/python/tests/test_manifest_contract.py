# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fail when the hand-written Python contract drifts from connector.yaml."""

import tomllib
from datetime import timedelta
from pathlib import Path

import dex
import yaml
from dex_connectors.sdk import default_connector_step_execute_retry_policy

from dex_connectors.pagerduty import GET_INCIDENT_DEFINITION, LIST_INCIDENTS_DEFINITION
from dex_connectors.pagerduty.pagerdutymock import (
    Mock,
    get_incident_default,
    list_incidents_default,
)
from examples.incident_review.flow import FLOW_TYPE, IncidentReviewFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    package = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + package["project"]["version"]
    assert package["project"]["name"] == "dex-connector-pagerduty"
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    assert set(operations) == {"listIncidents", "getIncident"}
    for definition in (LIST_INCIDENTS_DEFINITION, GET_INCIDENT_DEFINITION):
        operation = operations[definition.operation.operation_id]
        assert definition.operation.connector_id == manifest["metadata"]["name"]
        assert operation["kind"] == "query"
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        assert operation["execution"] == {"executeMethodTimeout": "30s", "durability": "async"}
        defaults = definition.step_defaults
        assert defaults.execute_method_timeout == timedelta(seconds=30)
        assert defaults.execute_durability == 2
        assert defaults.execute_retry == default_connector_step_execute_retry_policy()
    page = list_incidents_default().value
    assert page is not None
    assert operations["listIncidents"]["mocks"][0]["output"] == {
        "incidents": [
            {"id": incident.id, "title": incident.title, "status": incident.status}
            for incident in page.incidents
        ],
        "limit": page.limit,
        "offset": page.offset,
        "more": page.more,
    }
    incident = get_incident_default().value
    assert incident is not None
    assert operations["getIncident"]["mocks"][0]["output"] == {
        "id": incident.id,
        "title": incident.title,
        "status": incident.status,
    }


def test_example_registers_with_mock_connection() -> None:
    flow = IncidentReviewFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)


def test_factory_parameters_match_declared_branch_fields() -> None:
    import inspect
    import json

    import dex_connectors.pagerduty as package

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
