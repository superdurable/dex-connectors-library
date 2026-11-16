# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A Flow that records one HubSpot CRM record change, started by the crmObjectChanged Trigger.

Every contact, company, or deal of the picked object type that is created or changed starts one Flow,
which reads the record back and records its current state.
"""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    QueryResult,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.hubspot import (
    CRM_OBJECT_CHANGE_CREATED,
    CRM_OBJECT_CHANGE_UPDATED,
    Connection,
    CRMObject,
    CRMObjectChangedEvent,
    GetObjectInput,
    define_crm_object_changed_trigger_binding,
    new_get_object_step,
)

CONNECTION_NAME = "hubspot-crm-watcher"
"""The static Dex Web connection for HubSpot."""
CRM_OBJECT_CHANGED_TRIGGER_BINDING = "crm-object-changes"
"""The stable crmObjectChanged binding that starts this Flow."""
FLOW_TYPE = "HubSpotCRMObjectChangePython"
DELIVERED_PROPERTIES = (
    "email",
    "firstname",
    "lastname",
    "name",
    "domain",
    "dealname",
    "dealstage",
    "pipeline",
    "hubspot_owner_id",
)
"""The properties each delivery and read-back returns; HubSpot ignores a name the type does not define."""


@dataclass(frozen=True, slots=True)
class CRMObjectChange:
    """One crmObjectChanged delivery."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    """The stable Trigger event ID that also names the Flow."""
    change: CRMObjectChangedEvent = field(kw_only=True)
    """The delivered change."""


@dataclass(frozen=True, slots=True)
class ChangeOutcome:
    """The Flow result and the value of its outcome Attribute."""

    object_type: str = field(metadata={"dex_json_name": "objectType"})
    """The watched object type."""
    object_id: str = field(metadata={"dex_json_name": "objectId"})
    """The changed record."""
    change_kind: str = field(metadata={"dex_json_name": "changeKind"})
    """The delivered change kind."""
    current: CRMObject = field(kw_only=True)
    """The record read back from HubSpot."""


crm_object_change_attribute = dex.Attribute("hubspot-crm-object-change", CRMObjectChange)
crm_object_change_outcome_attribute = dex.Attribute("hubspot-crm-object-change-outcome", ChangeOutcome)


def accept_every_crm_object_change(event: TriggerEvent[CRMObjectChangedEvent]) -> bool:
    """Admit every change; an application narrows it, for example to created records."""
    return True


def resolve_flow_id(event: TriggerEvent[CRMObjectChangedEvent]) -> str:
    """Name one Flow per record version; a redelivered change finds its existing Flow."""
    return f"hubspot-crm-object-change-{event.id}"


def map_to_flow_input(event: TriggerEvent[CRMObjectChangedEvent]) -> CRMObjectChange:
    """Keep the Trigger event ID beside the delivered change."""
    return CRMObjectChange(event.id, change=event.payload)


def map_to_get_object_input(input: CRMObjectChange) -> GetObjectInput:
    """Read the changed record with the delivered properties."""
    return GetObjectInput(input.change.object_type, input.change.object.id, DELIVERED_PROPERTIES)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Validate and record the delivered record change, then read the record back."
class RecordCRMObjectChange(dex.Step[CRMObjectChange]):
    """Validate and persist the delivered change."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordCRMObjectChange"

    def execute(self, context: dex.Context, input: CRMObjectChange) -> dex.StepDecision:
        """Store the change and read the record back."""
        if not input.trigger_event_id.strip():
            return dex.force_fail("triggerEventId is required")
        if not input.change.object_type or not input.change.object.id.strip():
            return dex.force_fail("change.objectType and change.object.id are required")
        if input.change.change_kind not in (CRM_OBJECT_CHANGE_CREATED, CRM_OBJECT_CHANGE_UPDATED):
            return dex.force_fail("change.changeKind must be created or updated")
        crm_object_change_attribute.set(context, input)
        return dex.go_to(dex.StepRef("ReadChangedObject"), input)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the record read back from HubSpot as the outcome and complete the Flow."
class CompleteCRMObjectChange(dex.Step[QueryResult[CRMObject]]):
    """Persist the record read back and complete."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "CompleteCRMObjectChange"

    def execute(self, context: dex.Context, input: QueryResult[CRMObject]) -> dex.StepDecision:
        """Store the outcome and complete the Flow with it."""
        change = crm_object_change_attribute.get(context)
        outcome = ChangeOutcome(
            change.change.object_type, change.change.object.id, change.change.change_kind, current=input.value
        )
        crm_object_change_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


class CRMObjectChangeFlow(dex.Flow[CRMObjectChange]):
    """Records one HubSpot CRM record change."""

    def __init__(self, connection: Connection) -> None:
        """Bind the HubSpot connection that reads changed records at registration time."""
        self.record = RecordCRMObjectChange()
        self.complete = CompleteCRMObjectChange()
        self.read = new_get_object_step(
            step_type="ReadChangedObject",
            step_input_type=CRMObjectChange,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": (
                    "Read the changed record back, so the outcome holds its current values rather than "
                    "the search snapshot."
                ),
            },
            connection=connection,
            connection_name="hubspot-crm-watcher",
            map_to_operation_input=map_to_get_object_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Keep the application Flow identity stable across Worker versions."""
        return "HubSpotCRMObjectChangePython"

    def get_steps(self) -> dex.StepList[CRMObjectChange]:
        """Record the change, read the record back, and complete."""
        return dex.StepList.start_step(self.record).other_steps(self.read, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(crm_object_change_attribute, crm_object_change_outcome_attribute)

    # Declares the crmObjectChanged binding and its object type picker for Dex Web. The Dex analyzer
    # requires the method body to be one return statement, so it has no docstring.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_crm_object_changed_trigger_binding(
                connection_name="hubspot-crm-watcher",
                binding_name="crm-object-changes",
                configuration_ui=ConnectorConfigurationUI(
                    units=(
                        ConnectorUIUnit(
                            id="watchedObjectType",
                            unit_id="objectTypePicker",
                            label="Watched object type",
                            description="Choose whether created and changed contacts, companies, or deals "
                            "start this Flow; the unit saves HubSpot's object name. The connection's private "
                            "app or OAuth app needs the matching crm.objects.<type>.read scope, which the "
                            "HubSpot authorization guide already lists. It cannot be blank. The first start "
                            "records existing records as a baseline and starts no Flow for them. Restart "
                            "the Worker after saving; a changed object type records a new baseline.",
                            required=True,
                            bindings=(ConnectorUIBinding(port="objectType", json_pointer="/objectType"),),
                        ),
                    )
                ),
            )
        ]
