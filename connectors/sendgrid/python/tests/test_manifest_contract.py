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

import dex_connectors.sendgrid as package
from dex_connectors.sendgrid import GET_STATS_DEFINITION, LIST_TEMPLATES_DEFINITION
from dex_connectors.sendgrid.sendgridmock import Mock, get_stats_default_answer, list_templates_default_answer
from examples.demo.flow import FLOW_TYPE, DemoFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + project["project"]["version"]
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    for definition in (GET_STATS_DEFINITION, LIST_TEMPLATES_DEFINITION):
        operation = operations[definition.operation.operation_id]
        assert operation["kind"] == "query"
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        assert operation["execution"] == {"executeMethodTimeout": "30s", "durability": "async"}
        assert definition.step_defaults.execute_method_timeout == timedelta(seconds=30)
        assert definition.step_defaults.execute_durability == 2
        assert definition.step_defaults.execute_retry == default_connector_step_execute_retry_policy()
    stats = get_stats_default_answer().value
    templates = list_templates_default_answer().value
    assert stats is not None and templates is not None
    assert operations["getStats"]["mocks"][0]["output"] == {
        "days": [
            {"date": day.date, "requests": day.requests, "delivered": day.delivered} for day in stats.days
        ]
    }
    assert operations["listTemplates"]["mocks"][0]["output"] == {
        "templates": [
            {"id": template.id, "name": template.name, "generation": template.generation}
            for template in templates.templates
        ],
        "count": templates.count,
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
    flow = DemoFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
