# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check the new-row intake example's binding, configuration UI guidance, and Trigger mappings."""

from datetime import UTC, datetime

import httpx
from dex_connectors.sdk import ConnectionRef, StaticCredentialProvider, TriggerEvent

from dex_connectors.google_spreadsheet import (
    Client,
    Config,
    Connection,
    GetValuesInput,
    RowAddedEvent,
    new_connection,
)
from examples.new_row_intake.flow import (
    CONNECTION_NAME,
    FLOW_TYPE,
    NEW_ROW_TRIGGER_BINDING,
    READ_ADDED_ROW_STEP_TYPE,
    AddedRow,
    NewRowIntakeFlow,
    accept_added_row,
    map_to_flow_input,
    map_to_get_values_input,
    resolve_flow_id,
)

DETECTED_AT = datetime(2026, 11, 15, 9, 0, tzinfo=UTC)
EVENT = TriggerEvent(
    "row-12-0f3a9c1d2b4e5f60",
    DETECTED_AT,
    RowAddedEvent("sheet-1", "Leads", 12, {"email": "ada@example.test"}, ("ada@example.test",)),
)


def _connection() -> Connection:
    """A connection that never sends: these tests only build the Flow."""
    reference = ConnectionRef("google", CONNECTION_NAME)
    transport = httpx.MockTransport(lambda request: httpx.Response(500))
    client = Client(Config(), StaticCredentialProvider({}), httpx.AsyncClient(transport=transport))
    return new_connection(client, reference)


def test_flow_declares_one_static_binding_with_picker_units() -> None:
    flow = NewRowIntakeFlow(_connection())
    assert flow.get_flow_type() == FLOW_TYPE
    (binding,) = flow.get_connector_trigger_bindings()
    binding.validate()
    assert (binding.connection_name, binding.binding_name) == (CONNECTION_NAME, NEW_ROW_TRIGGER_BINDING)
    assert binding.definition.trigger.trigger_name == "rowAdded"
    assert binding.configuration_ui is not None
    units = {unit.id: unit for unit in binding.configuration_ui.units}
    assert list(units) == ["spreadsheet", "tab"]
    spreadsheet, tab = units["spreadsheet"], units["tab"]
    assert (spreadsheet.unit_id, spreadsheet.label, spreadsheet.required) == (
        "spreadsheetPicker",
        "Spreadsheet",
        True,
    )
    assert [(item.port, item.json_pointer) for item in spreadsheet.bindings] == [
        ("spreadsheetId", "/spreadsheetId")
    ]
    assert "https://docs.google.com/spreadsheets/d/" in spreadsheet.description
    assert "Google Picker" in spreadsheet.description and "cannot be blank" in spreadsheet.description
    assert (tab.unit_id, tab.label, tab.required) == ("sheetTabPicker", "Tab", True)
    assert [(item.port, item.json_pointer) for item in tab.bindings] == [
        ("spreadsheetId", "/spreadsheetId"),
        ("tab", "/sheetName"),
    ]
    assert "Row 1 must hold the column headers" in tab.description and "cannot be blank" in tab.description


def test_mappings_keep_one_flow_per_event_id() -> None:
    assert accept_added_row(EVENT)
    assert resolve_flow_id(EVENT) == "google-sheets-row-row-12-0f3a9c1d2b4e5f60"
    assert map_to_flow_input(EVENT) == AddedRow(
        "row-12-0f3a9c1d2b4e5f60", "sheet-1", "Leads", 12, {"email": "ada@example.test"}, DETECTED_AT
    )


def test_flow_reads_the_added_row_back_through_get_values() -> None:
    flow = NewRowIntakeFlow(_connection())
    assert flow.read.get_step_type() == READ_ADDED_ROW_STEP_TYPE
    row = map_to_flow_input(EVENT)
    assert map_to_get_values_input(row) == GetValuesInput("sheet-1", "'Leads'!12:12")
    quoted = AddedRow("event", "sheet-1", "Bob's leads", 7, {}, DETECTED_AT)
    assert map_to_get_values_input(quoted) == GetValuesInput("sheet-1", "'Bob''s leads'!7:7")
