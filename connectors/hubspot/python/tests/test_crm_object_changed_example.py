# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the crmObjectChanged example Flow and project runner wiring, without Dex."""

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

import dex_connectors.hubspot as hubspot
from dex_connectors.hubspot import (
    CRMObject,
    CRMObjectChangedEvent,
    ProjectCRMObjectChangedTriggerRoute,
    new_project_crm_object_trigger_runner,
)
from examples.crm_object_changed.flow import (
    CONNECTION_NAME,
    CRM_OBJECT_CHANGED_TRIGGER_BINDING,
    DELIVERED_PROPERTIES,
    CRMObjectChange,
    CRMObjectChangeFlow,
    accept_every_crm_object_change,
    map_to_flow_input,
    map_to_get_object_input,
    resolve_flow_id,
)

MODIFIED_AT = datetime(2026, 10, 25, 10, 0, 2, tzinfo=UTC)
CHANGE = TriggerEvent(
    "hubspot-38f57040bd071dc31d72af8e1d80ac42",
    MODIFIED_AT,
    CRMObjectChangedEvent(
        "deals", "created", MODIFIED_AT, object=CRMObject("deals", "1002", {"dealname": "New"})
    ),
)


class DiscardingTarget:
    async def handle_trigger(
        self, context: TriggerContext, event: TriggerEvent[CRMObjectChangedEvent]
    ) -> None:
        return None


def test_each_change_maps_to_one_flow_that_reads_the_record_back() -> None:
    assert accept_every_crm_object_change(CHANGE)
    assert resolve_flow_id(CHANGE) == "hubspot-crm-object-change-hubspot-38f57040bd071dc31d72af8e1d80ac42"
    flow_input = map_to_flow_input(CHANGE)
    assert flow_input == CRMObjectChange(CHANGE.id, change=CHANGE.payload)
    read = map_to_get_object_input(flow_input)
    assert (read.object_type, read.object_id, read.properties) == ("deals", "1002", DELIVERED_PROPERTIES)


def test_flow_declares_the_binding_with_its_required_object_type_picker() -> None:
    connection = hubspot.new_connection(
        hubspot.Client(hubspot.Config(), StaticCredentialProvider({})),
        ConnectionRef("hubspot", CONNECTION_NAME),
    )
    (binding,) = CRMObjectChangeFlow(connection).get_connector_trigger_bindings()
    assert (binding.connection_name, binding.binding_name) == (
        CONNECTION_NAME,
        CRM_OBJECT_CHANGED_TRIGGER_BINDING,
    )
    assert binding.definition.trigger.trigger_name == "crmObjectChanged"
    assert binding.configuration_ui is not None
    (unit,) = binding.configuration_ui.units
    assert (unit.unit_id, unit.required) == (hubspot.UI_UNIT_OBJECT_TYPE_PICKER, True)
    assert [(item.port, item.json_pointer) for item in unit.bindings] == [("objectType", "/objectType")]
    assert "crm.objects.<type>.read" in unit.description


def project(bindings: list[dict[str, Any]], settings: dict[str, Any]) -> LoadedProject:
    scope = Scope("hubspot-unit", "live")
    objects = InMemoryObjectStore()
    configuration = Configuration(
        scope,
        1,
        (
            ConnectionConfiguration(
                "hubspot", CONNECTION_NAME, "hubspot", "hubspot", settings, "private-app-token"
            ),
        ),
        tuple(
            {
                "connectorId": "hubspot",
                "connectionName": CONNECTION_NAME,
                "triggerName": "crmObjectChanged",
                "bindingName": CRM_OBJECT_CHANGED_TRIGGER_BINDING,
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
        ([], {}, "has no saved object type; pick one in Dex Web"),
        ([{"objectType": "tickets"}], {}, "object type must be contacts, companies, or deals"),
        ([{"objectType": "deals", "unknown": True}], {}, "configuration"),
    ],
)
async def test_project_runner_refuses_invalid_bindings_and_settings(
    bindings: list[dict[str, Any]], settings: dict[str, Any], message: str
) -> None:
    route = ProjectCRMObjectChangedTriggerRoute(CRM_OBJECT_CHANGED_TRIGGER_BINDING, DiscardingTarget())
    with pytest.raises(ValueError, match=message):
        await new_project_crm_object_trigger_runner(project(bindings, settings), CONNECTION_NAME, [route])


async def test_project_runner_accepts_a_saved_object_type() -> None:
    route = ProjectCRMObjectChangedTriggerRoute(CRM_OBJECT_CHANGED_TRIGGER_BINDING, DiscardingTarget())
    runner = await new_project_crm_object_trigger_runner(
        project([{"objectType": "deals"}], {"pollInterval": 5_000_000_000}), CONNECTION_NAME, [route]
    )
    await runner.aclose()
