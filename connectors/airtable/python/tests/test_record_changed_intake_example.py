# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the record-changed intake example's Flow wiring, mapping, and binding UI."""

from datetime import UTC, datetime

import dex
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider, TriggerEvent

from dex_connectors.airtable import (
    UI_UNIT_BASE_PICKER,
    UI_UNIT_LAST_MODIFIED_TIME_FIELD_PICKER,
    UI_UNIT_TABLE_PICKER,
    Client,
    Config,
    Connection,
    Credentials,
    GetRecordInput,
    Record,
    RecordChangedEvent,
    new_connection,
)
from examples.record_changed_intake.flow import (
    CONNECTION_NAME,
    FLOW_TYPE,
    RECORD_CHANGED_TRIGGER_BINDING,
    ChangedRecord,
    Intake,
    RecordChangedIntakeFlow,
    accept_changed_record,
    complete_with_current_record,
    map_to_flow_input,
    map_to_read_back_input,
    resolve_flow_id,
)

CHANGED_AT = datetime(2026, 11, 15, 9, 0, 0, 123000, tzinfo=UTC)
EVENT = TriggerEvent(
    "recNewLead0000001-20261115T090000123000000Z",
    CHANGED_AT,
    RecordChangedEvent(
        base_id="appIntakeBase0001",
        table_id="tblIntakeLeads001",
        last_modified_time=CHANGED_AT,
        is_created_record=True,
        record=Record("recNewLead0000001", CHANGED_AT, {"Email": "lead@example.test"}),
    ),
)


def intake_connection(name: str = CONNECTION_NAME) -> Connection:
    reference = ConnectionRef("airtable", name)
    # A placeholder token, never sent: these tests build the Flow without calling Airtable.
    credentials = StaticCredentialProvider({reference: Credentials(SecretString("placeholder"))})
    return new_connection(Client(Config(), credentials), reference)


def test_each_change_names_one_flow_and_carries_the_record() -> None:
    assert accept_changed_record(EVENT)
    assert resolve_flow_id(EVENT) == "airtable-record-change-recNewLead0000001-20261115T090000123000000Z"
    assert map_to_flow_input(EVENT) == ChangedRecord(
        event_id=EVENT.id,
        base_id="appIntakeBase0001",
        table_id="tblIntakeLeads001",
        record_id="recNewLead0000001",
        is_created_record=True,
        last_modified_time=CHANGED_AT,
        fields={"Email": "lead@example.test"},
    )


def test_flow_registers_and_declares_the_binding_with_its_three_pickers() -> None:
    flow = RecordChangedIntakeFlow(intake_connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
    (binding,) = flow.get_connector_trigger_bindings()
    assert (binding.connection_name, binding.binding_name) == (
        CONNECTION_NAME,
        RECORD_CHANGED_TRIGGER_BINDING,
    )
    assert binding.definition.trigger.trigger_name == "recordChanged"
    assert binding.configuration_ui is not None
    base, table, field = binding.configuration_ui.units
    assert [unit.unit_id for unit in (base, table, field)] == [
        UI_UNIT_BASE_PICKER,
        UI_UNIT_TABLE_PICKER,
        UI_UNIT_LAST_MODIFIED_TIME_FIELD_PICKER,
    ]
    assert all(unit.required and unit.description for unit in (base, table, field))
    assert [(item.port, item.json_pointer) for item in field.bindings] == [
        ("baseId", "/baseId"),
        ("tableId", "/tableId"),
        ("fieldId", "/fieldId"),
        ("fieldName", "/fieldName"),
    ]
    assert "https://airtable.com/" in base.description
    assert "Include time" in field.description and "never start Flows" in table.description


def test_flow_reads_the_changed_record_back_and_keeps_its_current_fields() -> None:
    change = map_to_flow_input(EVENT)
    assert map_to_read_back_input(change) == GetRecordInput(
        base_id="appIntakeBase0001", table_id_or_name="tblIntakeLeads001", record_id="recNewLead0000001"
    )
    current = Record("recNewLead0000001", CHANGED_AT, {"Email": "lead@example.test", "Status": "New"})
    completed = complete_with_current_record(Intake(change), current)
    assert completed == Intake(change, {"Email": "lead@example.test", "Status": "New"}, False, True)
    with pytest.raises(ValueError, match="instead of the changed record"):
        complete_with_current_record(Intake(change), Record("recOtherLead00001", CHANGED_AT, {}))


def test_flow_requires_the_polled_connection() -> None:
    with pytest.raises(ValueError, match="airtable-intake"):
        RecordChangedIntakeFlow(intake_connection("airtable-other"))
