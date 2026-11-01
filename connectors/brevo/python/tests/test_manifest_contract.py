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

import dex_connectors.brevo as package
from dex_connectors.brevo import CREATE_OR_UPDATE_CONTACT_DEFINITION, GET_CONTACT_DEFINITION
from dex_connectors.brevo.brevomock import (
    Mock,
    create_or_update_contact_default_answer,
    get_contact_default_answer,
)
from examples.contact_sync.flow import FLOW_TYPE, ContactSyncFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + project["project"]["version"]
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    for definition, kind in (
        (CREATE_OR_UPDATE_CONTACT_DEFINITION, "mutation"),
        (GET_CONTACT_DEFINITION, "query"),
    ):
        operation = operations[definition.operation.operation_id]
        assert operation["kind"] == kind
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        assert operation["execution"] == {"executeMethodTimeout": "30s", "durability": "async"}
        assert definition.step_defaults.execute_method_timeout == timedelta(seconds=30)
        assert definition.step_defaults.execute_retry == default_connector_step_execute_retry_policy()
    saved = create_or_update_contact_default_answer().value
    contact = get_contact_default_answer().value
    assert saved is not None and contact is not None
    assert operations["createOrUpdateContact"]["mocks"][0]["output"] == {
        "id": saved.id,
        "email": saved.email,
        "created": saved.created,
    }
    assert operations["getContact"]["mocks"][0]["output"] == {"id": contact.id, "email": contact.email}


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
    flow = ContactSyncFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
