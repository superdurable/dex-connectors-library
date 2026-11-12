# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Google Contacts Python Flow with a mock connection."""

import json
from pathlib import Path

import dex

import dex_connectors.google_contacts as google_contacts
from dex_connectors.google_contacts.contactsmock import Mock
from examples.contacts.flow import FLOW_TYPE, ContactsFlow
from examples.creation.flow import FLOW_TYPE as CREATION_FLOW_TYPE
from examples.creation.flow import CreationFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and checked-in Flow agree on their Dex identities."""
    flow = ContactsFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)


def test_creation_example_registers_with_mock_connection() -> None:
    """The generated create Step and checked-in Flow agree on their Dex identities."""
    flow = CreationFlow(Mock().connection())
    assert flow.get_flow_type() == CREATION_FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)


def test_input_schemas_let_callers_omit_optional_fields() -> None:
    document = json.loads((Path(google_contacts.__file__).parent / "dex-connector.json").read_text())
    schemas = {factory["operationId"]: factory["inputSchema"] for factory in document["factories"]}
    # Zero pageSize selects 100, blank pageToken reads the first page, blank names and emails are omitted.
    assert "required" not in schemas["listContacts"]
    assert schemas["createContact"]["required"] == ["givenName"]
