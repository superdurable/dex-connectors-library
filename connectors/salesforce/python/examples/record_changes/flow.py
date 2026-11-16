# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A Flow that records one Salesforce record change, started by the recordModifiedPolled Trigger.

Every record of the picked object that is created or modified starts one Flow, which reads the record
back and records its current state, or records that it was deleted before the read.
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

from dex_connectors.salesforce import (
    RECORD_CHANGE_CREATED,
    RECORD_CHANGE_UPDATED,
    Connection,
    GetRecordInput,
    Record,
    RecordModifiedEvent,
    define_record_modified_polled_trigger_binding,
    new_get_record_step,
)

CONNECTION_NAME = "salesforce-crm"
"""The static Dex Web connection for Salesforce, shared with the record_lookup example."""
RECORD_MODIFIED_TRIGGER_BINDING = "record-changes"
"""The stable recordModifiedPolled binding that starts this Flow."""
FLOW_TYPE = "SalesforceRecordChangePython"
"""The stable Flow identity each record change starts."""


@dataclass(frozen=True, slots=True)
class RecordChange:
    """One recordModifiedPolled delivery."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    """The stable Trigger event ID that also names the Flow."""
    change: RecordModifiedEvent = field(kw_only=True)
    """The delivered record change."""


@dataclass(frozen=True, slots=True)
class ChangeOutcome:
    """The Flow result and the value of its outcome Attribute."""

    s_object_type: str = field(metadata={"dex_json_name": "sObjectType"})
    """The watched object."""
    record_id: str = field(metadata={"dex_json_name": "recordId"})
    """The changed record."""
    change_kind: str = field(metadata={"dex_json_name": "changeKind"})
    """The delivered change kind."""
    is_deleted: bool = field(default=False, metadata={"dex_json_name": "isDeleted"})
    """Whether the record was deleted before the Flow read it back."""
    current: Record | None = field(default=None, metadata={"dex_json_omit_default": True})
    """The record read back from Salesforce; None when is_deleted is True."""


record_change_attribute = dex.Attribute("salesforce-record-change", RecordChange)
record_change_outcome_attribute = dex.Attribute("salesforce-record-change-outcome", ChangeOutcome)


def accept_every_record_change(event: TriggerEvent[RecordModifiedEvent]) -> bool:
    """Admit every change; an application narrows it, for example to created records."""
    return True


def resolve_flow_id(event: TriggerEvent[RecordModifiedEvent]) -> str:
    """Name one Flow per change; a redelivered change finds its existing Flow."""
    return f"salesforce-record-change-{event.id}"


def map_to_flow_input(event: TriggerEvent[RecordModifiedEvent]) -> RecordChange:
    """Keep the Trigger event ID beside the delivered change."""
    return RecordChange(event.id, change=event.payload)


def map_to_get_record_input(input: RecordChange) -> GetRecordInput:
    """Read back every field the delivered change carries, in a stable order."""
    fields = tuple(sorted((input.change.record.fields or {}).keys()))
    return GetRecordInput(input.change.s_object_type, input.change.record_id, fields)


def _change_outcome(context: dex.Context, current: Record | None) -> ChangeOutcome:
    """Build the outcome of the recorded change; a None current record means it was deleted."""
    change = record_change_attribute.get(context).change
    return ChangeOutcome(change.s_object_type, change.record_id, change.change_kind, current is None, current)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the delivered record change, then read the record back from Salesforce."
class RecordRecordChange(dex.Step[RecordChange]):
    """Validate and persist the delivered change."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordRecordChange"

    def execute(self, context: dex.Context, input: RecordChange) -> dex.StepDecision:
        """Store the change and read the record back."""
        if not input.trigger_event_id.strip():
            return dex.force_fail("triggerEventId is required")
        if not input.change.s_object_type.strip() or not input.change.record_id.strip():
            return dex.force_fail("change.sObjectType and change.recordId are required")
        if input.change.change_kind not in (RECORD_CHANGE_CREATED, RECORD_CHANGE_UPDATED):
            return dex.force_fail("change.changeKind must be created or updated")
        record_change_attribute.set(context, input)
        return dex.go_to(dex.StepRef("ReadChangedRecord"), input)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the record read back from Salesforce as the outcome and complete the Flow."
class CompleteRecordChange(dex.Step[QueryResult[Record]]):
    """Persist the record read back and complete."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "CompleteRecordChange"

    def execute(self, context: dex.Context, input: QueryResult[Record]) -> dex.StepDecision:
        """Store the outcome and complete the Flow with it."""
        outcome = _change_outcome(context, input.value)
        record_change_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record that the record was deleted before the read-back and complete the Flow."
class CompleteDeletedRecord(dex.Step[QueryResult[Record]]):
    """Record a deletion observed by the read-back and complete."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "CompleteDeletedRecord"

    def execute(self, context: dex.Context, input: QueryResult[Record]) -> dex.StepDecision:
        """Store a deleted outcome and complete the Flow with it."""
        outcome = _change_outcome(context, None)
        record_change_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


class RecordChangeFlow(dex.Flow[RecordChange]):
    """Records one Salesforce record change."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Salesforce connection that reads changed records at registration time."""
        self.record = RecordRecordChange()
        self.complete = CompleteRecordChange()
        self.complete_deleted = CompleteDeletedRecord()
        self.read = new_get_record_step(
            step_type="ReadChangedRecord",
            step_input_type=RecordChange,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": (
                    "Read the changed record back, so the outcome holds its current values rather than "
                    "the polled snapshot."
                ),
            },
            connection=connection,
            connection_name="salesforce-crm",
            map_to_operation_input=map_to_get_record_input,
            found=self.complete,
            # Business reaction: a record deleted after the poll read it completes as deleted.
            not_found=self.complete_deleted,
        )

    def get_flow_type(self) -> str:
        """Keep the application Flow identity stable across Worker versions."""
        return "SalesforceRecordChangePython"

    def get_steps(self) -> dex.StepList[RecordChange]:
        """Record the change, read the record back, and complete."""
        return dex.StepList.start_step(self.record).other_steps(
            self.read, self.complete, self.complete_deleted
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(record_change_attribute, record_change_outcome_attribute)

    # Declares the recordModifiedPolled binding with its object and field units for Dex Web. The Dex
    # analyzer requires the method body to be one return statement, so it has no docstring.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_record_modified_polled_trigger_binding(
                connection_name="salesforce-crm",
                binding_name="record-changes",
                configuration_ui=ConnectorConfigurationUI(
                    units=(
                        ConnectorUIUnit(
                            id="watchedObject",
                            unit_id="sObjectPicker",
                            label="Watched object",
                            description="Choose the object whose created and modified records start this "
                            "Flow, such as Opportunity, or sign in at https://login.salesforce.com "
                            "(https://test.salesforce.com for a sandbox) and enter a custom object API name "
                            "from Setup > Object Manager > the object > API Name, such as Invoice__c; the "
                            "unit saves the non-secret object API name, and it is required. The first start "
                            "records existing records as a baseline and starts no Flow for them. Restart the "
                            "Worker after saving; a changed object records a new baseline.",
                            required=True,
                            bindings=(ConnectorUIBinding(port="sObjectType", json_pointer="/sObjectType"),),
                        ),
                        ConnectorUIUnit(
                            id="deliveredFields",
                            unit_id="fieldNamesInput",
                            label="Delivered fields",
                            description="Enter the comma-separated field API names each delivered change "
                            "carries and the Flow reads back, such as Name, StageName, after signing in at "
                            "https://login.salesforce.com (https://test.salesforce.com for a sandbox) under "
                            "Setup > Object Manager > the object > Fields & Relationships > Field Name "
                            "column; the unit saves the non-secret list. Blank delivers only Id, "
                            "CreatedDate, LastModifiedDate, and SystemModstamp. Restart the Worker after "
                            "saving; the cursor is kept.",
                            required=False,
                            bindings=(ConnectorUIBinding(port="fieldNames", json_pointer="/fieldNames"),),
                        ),
                    )
                ),
            )
        ]
