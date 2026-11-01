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

import dex_connectors.datadog as package
from dex_connectors.datadog import LIST_MONITORS_DEFINITION, QUERY_METRICS_DEFINITION
from dex_connectors.datadog.datadogmock import (
    Mock,
    list_monitors_default_answer,
    query_metrics_default_answer,
)
from examples.datadog_lookup.flow import FLOW_TYPE, DatadogLookupFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + project["project"]["version"]
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    for definition in (QUERY_METRICS_DEFINITION, LIST_MONITORS_DEFINITION):
        operation = operations[definition.operation.operation_id]
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        assert operation["execution"] == {"executeMethodTimeout": "30s", "durability": "async"}
        assert definition.step_defaults.execute_method_timeout == timedelta(seconds=30)
        assert definition.step_defaults.execute_retry == default_connector_step_execute_retry_policy()
    metrics = query_metrics_default_answer().value
    monitors = list_monitors_default_answer().value
    assert metrics is not None and monitors is not None
    assert operations["queryMetrics"]["mocks"][0]["output"] == {
        "status": metrics.status,
        "series": [{"metric": s.metric, "pointlist": [list(p) for p in s.pointlist]} for s in metrics.series],
    }
    assert operations["listMonitors"]["mocks"][0]["output"] == {
        "monitors": [
            {"id": m.id, "name": m.name, "type": m.type, "overall_state": m.overall_state}
            for m in monitors.monitors
        ]
    }


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
    flow = DatadogLookupFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
