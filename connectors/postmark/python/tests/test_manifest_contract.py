# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fail when the hand-written Python contract drifts from connector.yaml."""

import tomllib
from datetime import timedelta
from pathlib import Path

import dex
import yaml
from dex_connectors.sdk import default_connector_step_execute_retry_policy

from dex_connectors.postmark import GET_MESSAGE_DETAILS_DEFINITION, LIST_OUTBOUND_MESSAGES_DEFINITION
from dex_connectors.postmark.postmarkmock import (
    Mock,
    get_message_details_default,
    list_outbound_messages_default,
)
from examples.outbound_lookup.flow import FLOW_TYPE, OutboundLookupFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    package = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + package["project"]["version"]
    assert package["project"]["name"] == "dex-connector-postmark"
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    assert set(operations) == {"listOutboundMessages", "getMessageDetails"}
    for definition in (LIST_OUTBOUND_MESSAGES_DEFINITION, GET_MESSAGE_DETAILS_DEFINITION):
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
    page = list_outbound_messages_default().value
    assert page is not None
    assert operations["listOutboundMessages"]["mocks"][0]["output"] == {
        "totalCount": page.total_count,
        "messages": [
            {"messageId": message.message_id, "subject": message.subject, "status": message.status}
            for message in page.messages
        ],
    }
    message = get_message_details_default().value
    assert message is not None
    assert operations["getMessageDetails"]["mocks"][0]["output"] == {
        "messageId": message.message_id,
        "subject": message.subject,
        "status": message.status,
    }


def test_example_registers_with_mock_connection() -> None:
    flow = OutboundLookupFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
