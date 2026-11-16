# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the databaseRowChanged example's Flow wiring and mapping."""

from datetime import UTC, datetime

import dex
import httpx
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider, TriggerEvent

from dex_connectors.notion import (
    Client,
    Config,
    Credentials,
    DatabaseRowChangedEvent,
    Page,
    Parent,
    new_connection,
)
from examples.row_changed.flow import (
    CONNECTION_NAME,
    DATABASE_ROW_CHANGED_TRIGGER_BINDING,
    DatabaseRowChangeFlow,
    RowChangeInput,
    map_to_flow_input,
    map_to_get_page_input,
    resolve_flow_id,
    validate_row_change_input,
)

ROW = Page(
    id="2a4d6f80-1b3c-4d5e-8f70-0000000000a1",
    title="Standup",
    parent=Parent(type="data_source_id"),
    properties={},
)
CHANGE = DatabaseRowChangedEvent(
    data_source_id="2a4d6f80-1b3c-4d5e-8f70-112233445566", change_kind="created", row=ROW
)
EVENT = TriggerEvent(
    "notion-row-e0e716fa48c2158e8ccee9565192273e", datetime(2026, 11, 2, 9, 31, tzinfo=UTC), CHANGE
)


def new_flow() -> DatabaseRowChangeFlow:
    reference = ConnectionRef("notion", CONNECTION_NAME)
    credentials = StaticCredentialProvider({reference: Credentials(SecretString("ntn_fake_token"))})
    return DatabaseRowChangeFlow(
        new_connection(Client(Config(), credentials, httpx.AsyncClient()), reference)
    )


def test_each_row_version_names_one_flow_and_reads_its_row() -> None:
    assert resolve_flow_id(EVENT) == "notion-row-change-notion-row-e0e716fa48c2158e8ccee9565192273e"
    flow_input = map_to_flow_input(EVENT)
    assert flow_input == RowChangeInput(trigger_event_id=EVENT.id, change=CHANGE)
    page_input = map_to_get_page_input(flow_input)
    assert (page_input.page_id, page_input.skip_content) == (ROW.id, True)
    assert validate_row_change_input(flow_input) == ""


def test_invalid_deliveries_are_refused() -> None:
    assert validate_row_change_input(RowChangeInput(trigger_event_id=" ", change=CHANGE))
    unknown = DatabaseRowChangedEvent(data_source_id="ds", change_kind="moved", row=ROW)
    assert "created or updated" in validate_row_change_input(RowChangeInput("event", unknown))
    missing = DatabaseRowChangedEvent(
        data_source_id="ds", change_kind="created", row=Page(parent=Parent(type=""))
    )
    assert "row.id" in validate_row_change_input(RowChangeInput("event", missing))


def test_flow_registers_and_declares_the_binding_with_its_data_source_picker() -> None:
    flow = new_flow()
    dex.Registry([flow], allow_async_handlers=True)
    (binding,) = flow.get_connector_trigger_bindings()
    assert binding.binding_name == DATABASE_ROW_CHANGED_TRIGGER_BINDING
    assert binding.connection_name == CONNECTION_NAME
    assert binding.definition.trigger.trigger_name == "databaseRowChanged"
    assert binding.configuration_ui is not None
    (unit,) = binding.configuration_ui.units
    assert (unit.unit_id, unit.required) == ("dataSourcePicker", True)
    assert [(item.port, item.json_pointer) for item in unit.bindings] == [
        ("dataSourceId", "/dataSourceId"),
        ("dataSourceName", "/dataSourceName"),
    ]
    assert "https://app.notion.com" in unit.description and "baseline" in unit.description
