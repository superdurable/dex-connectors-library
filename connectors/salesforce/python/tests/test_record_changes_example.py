# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the record_changes example Flow and project runner wiring, without Dex."""

from datetime import UTC, datetime
from typing import Any

import pytest
from dex_connectors.sdk import ConnectionRef, StaticCredentialProvider, TriggerContext, TriggerEvent
from dex_connectors.sdk.projectconfig import (
    Configuration,
    ConfigurationStore,
    ConnectionConfiguration,
    ConnectionStore,
    InMemoryObjectStore,
    LoadedProject,
    Scope,
    SnapshotRef,
)

import dex_connectors.salesforce as salesforce
from dex_connectors.salesforce import (
    ProjectRecordModifiedPolledTriggerRoute,
    Record,
    RecordModifiedEvent,
    new_project_record_trigger_runner,
)
from examples.record_changes.flow import (
    CONNECTION_NAME,
    RECORD_MODIFIED_TRIGGER_BINDING,
    RecordChange,
    RecordChangeFlow,
    accept_every_record_change,
    map_to_flow_input,
    map_to_get_record_input,
    resolve_flow_id,
)

MODSTAMP = datetime(2026, 3, 2, 9, 0, 2, tzinfo=UTC)
RECORD_ID = "006RM0000000002AAA"
CHANGE = TriggerEvent(
    "sfdc-0c1a7c970b998bb6d0b3ed38dc0f74e7",
    MODSTAMP,
    RecordModifiedEvent(
        "Opportunity",
        RECORD_ID,
        change_kind="created",
        system_modstamp=MODSTAMP,
        record=Record("Opportunity", RECORD_ID, {"StageName": "Prospecting", "Id": RECORD_ID, "Name": "New"}),
    ),
)


class DiscardingTarget:
    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[RecordModifiedEvent]) -> None:
        return None


def test_each_change_maps_to_one_flow_that_reads_the_record_back() -> None:
    assert accept_every_record_change(CHANGE)
    assert resolve_flow_id(CHANGE) == "salesforce-record-change-sfdc-0c1a7c970b998bb6d0b3ed38dc0f74e7"
    flow_input = map_to_flow_input(CHANGE)
    assert flow_input == RecordChange(CHANGE.id, change=CHANGE.payload)
    read = map_to_get_record_input(flow_input)
    assert (read.s_object_type, read.record_id, read.fields) == (
        "Opportunity",
        RECORD_ID,
        ("Id", "Name", "StageName"),
    )


def test_flow_declares_the_binding_with_its_object_picker_and_field_list() -> None:
    connection = salesforce.new_connection(
        salesforce.Client(salesforce.Config(), StaticCredentialProvider({})),
        ConnectionRef("salesforce", CONNECTION_NAME),
    )
    (binding,) = RecordChangeFlow(connection).get_connector_trigger_bindings()
    assert (binding.connection_name, binding.binding_name) == (
        CONNECTION_NAME,
        RECORD_MODIFIED_TRIGGER_BINDING,
    )
    assert binding.definition.trigger.trigger_name == "recordModifiedPolled"
    assert binding.configuration_ui is not None
    picker, fields = binding.configuration_ui.units
    assert (picker.unit_id, picker.required) == (salesforce.UI_UNIT_S_OBJECT_PICKER, True)
    assert [(item.port, item.json_pointer) for item in picker.bindings] == [("sObjectType", "/sObjectType")]
    assert "https://login.salesforce.com" in picker.description and "baseline" in picker.description
    assert (fields.unit_id, fields.required) == (salesforce.UI_UNIT_FIELD_NAMES_INPUT, False)
    assert [(item.port, item.json_pointer) for item in fields.bindings] == [("fieldNames", "/fieldNames")]
    assert "Fields & Relationships" in fields.description and "Blank delivers only" in fields.description


def project(bindings: list[dict[str, Any]], settings: dict[str, Any]) -> LoadedProject:
    scope = Scope("salesforce-unit", "live")
    objects = InMemoryObjectStore()
    configuration = Configuration(
        scope,
        1,
        (
            ConnectionConfiguration(
                "salesforce", CONNECTION_NAME, "salesforce", "salesforce", settings, "salesforce-oauth"
            ),
        ),
        tuple(
            {
                "connectorId": "salesforce",
                "connectionName": CONNECTION_NAME,
                "triggerName": "recordModifiedPolled",
                "bindingName": RECORD_MODIFIED_TRIGGER_BINDING,
                "configuration": configuration,
            }
            for configuration in bindings
        ),
    )
    return LoadedProject(
        configuration,
        SnapshotRef("k", "1", "sha256:" + "0" * 64),
        ConnectionStore(objects, scope),
        ConfigurationStore(objects, scope),
    )


@pytest.mark.parametrize(
    ("bindings", "settings", "message"),
    [
        ([], {"pollInterval": 4_999_999_999}, "at least 5s"),
        ([], {"pollPageSize": 0}, "pollPageSize must be from 1 to 500"),
        ([], {}, "has no saved object; choose one in Dex Web Connections"),
        ([{"sObjectType": "Bad Name"}], {}, "sObjectType must be an object API name"),
        ([{"sObjectType": "Contact", "fieldNames": ["Account.Name"]}], {}, "without relationship dots"),
        ([{"sObjectType": "Contact", "fieldNames": "Name"}], {}, "must be a list"),
        ([{"sObjectType": "Contact", "unknown": True}], {}, "configuration"),
    ],
)
async def test_project_runner_refuses_invalid_bindings_and_settings(
    bindings: list[dict[str, Any]], settings: dict[str, Any], message: str
) -> None:
    route = ProjectRecordModifiedPolledTriggerRoute(RECORD_MODIFIED_TRIGGER_BINDING, DiscardingTarget())
    with pytest.raises(ValueError, match=message):
        await new_project_record_trigger_runner(project(bindings, settings), CONNECTION_NAME, [route])


async def test_project_runner_accepts_a_saved_object() -> None:
    route = ProjectRecordModifiedPolledTriggerRoute(RECORD_MODIFIED_TRIGGER_BINDING, DiscardingTarget())
    runner = await new_project_record_trigger_runner(
        project([{"sObjectType": "Opportunity", "fieldNames": ["StageName"]}], {}), CONNECTION_NAME, [route]
    )
    await runner.aclose()
