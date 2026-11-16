# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Every record created or changed in the picked Airtable table starts one Flow, which records the change
and reads the record back through the Airtable connection for review in Dex Web."""

from dataclasses import dataclass, field, replace
from datetime import datetime
from typing import Any

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    QueryResult,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.airtable import (
    Connection,
    GetRecordInput,
    Record,
    RecordChangedEvent,
    define_record_changed_trigger_binding,
    new_get_record_step,
)

FLOW_TYPE = "AirtableRecordChangedIntakePython"
CONNECTION_NAME = "airtable-intake"
RECORD_CHANGED_TRIGGER_BINDING = "intake-record-changed"
FLOW_ID_PREFIX = "airtable-record-change-"
_READ_BACK_STEP_TYPE = "ReadBackChangedRecord"


@dataclass(frozen=True, slots=True)
class ChangedRecord:
    """The Flow's start input: one change the recordChanged Trigger found."""

    event_id: str = field(metadata={"dex_json_name": "eventId"})
    """The Trigger event ID, such as recXXXXXXXXXXXXXX-20261115T090000123000000Z."""
    base_id: str = field(metadata={"dex_json_name": "baseId"})
    """The polled base ID."""
    table_id: str = field(metadata={"dex_json_name": "tableId"})
    """The polled table ID."""
    record_id: str = field(metadata={"dex_json_name": "recordId"})
    """The changed record's ID."""
    is_created_record: bool = field(metadata={"dex_json_name": "isCreatedRecord"})
    """Whether the record was created and not edited since."""
    last_modified_time: datetime = field(
        metadata={"dex_json_name": "lastModifiedTime", "dex_json_format": "rfc3339"}
    )
    """The record's Last modified time value, in UTC."""
    fields: dict[str, Any] = field(default_factory=dict)
    """Each non-empty field name and its cell value."""


@dataclass(frozen=True, slots=True)
class Intake:
    """The Flow's state and output: the reported change and the record as the Flow read it back."""

    change: ChangedRecord
    """The change the Trigger reported."""
    current_fields: dict[str, Any] = field(default_factory=dict, metadata={"dex_json_name": "currentFields"})
    """Each non-empty field name and its cell value when the Flow read the record back; empty until then
    and when the record was deleted."""
    is_removed: bool = field(default=False, metadata={"dex_json_name": "isRemoved"})
    """Whether the record was deleted after the change and before the Flow read it back."""
    is_completed: bool = field(default=False, metadata={"dex_json_name": "isCompleted"})
    """Whether the Flow finished reading the record back."""


intake_attribute = dex.Attribute("airtable-record-intake", Intake)


def accept_changed_record(event: TriggerEvent[RecordChangedEvent]) -> bool:
    """Admit every change the Trigger reports; an application narrows it, for example to new records."""
    return True


def resolve_flow_id(event: TriggerEvent[RecordChangedEvent]) -> str:
    """Name one Flow per change; a redelivered change finds its existing Flow."""
    return FLOW_ID_PREFIX + event.id


def map_to_flow_input(event: TriggerEvent[RecordChangedEvent]) -> ChangedRecord:
    """Copy the changed record into the Flow's start input."""
    change = event.payload
    return ChangedRecord(
        event_id=event.id,
        base_id=change.base_id,
        table_id=change.table_id,
        record_id=change.record.id,
        is_created_record=change.is_created_record,
        last_modified_time=change.last_modified_time,
        fields=dict(change.record.fields or {}),
    )


def map_to_read_back_input(change: ChangedRecord) -> GetRecordInput:
    """Read the changed record from its polled base and table."""
    return GetRecordInput(
        base_id=change.base_id, table_id_or_name=change.table_id, record_id=change.record_id
    )


def complete_with_current_record(intake: Intake, record: Record) -> Intake:
    """Store the read-back record's fields; raises ValueError when it is not the changed record."""
    if record.id != intake.change.record_id:
        raise ValueError(
            f"Airtable returned record {record.id} instead of the changed record {intake.change.record_id}"
        )
    return replace(intake, current_fields=dict(record.fields or {}), is_completed=True)


# dex:group group-id:intake group-label:"Intake"
# dex:explanation text:"Persist the changed Airtable record, then read it back from Airtable."
class RecordChangedRecord(dex.Step[ChangedRecord]):
    """Store the delivered change."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "RecordChangedRecord"

    def execute(self, context: dex.Context, input: ChangedRecord) -> dex.StepDecision:
        """Fail an incomplete delivery; otherwise save the change and read the record back."""
        if not input.event_id or not input.record_id or input.last_modified_time.year <= 1:
            return dex.force_fail("a changed record requires its event ID, record ID, and modification time")
        intake_attribute.set(context, Intake(change=input))
        return dex.go_to(dex.StepRef("ReadBackChangedRecord"), input)


# dex:group group-id:airtable group-label:"Airtable"
# dex:explanation text:"Store the record's current fields and complete the Flow."
class CompleteRecordIntake(dex.Step[QueryResult[Record]]):
    """Store the read-back record."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "CompleteRecordIntake"

    def execute(self, context: dex.Context, input: QueryResult[Record]) -> dex.StepDecision:
        """Complete with the record's current fields, or fail when Airtable returned another record."""
        try:
            completed = complete_with_current_record(intake_attribute.get(context), input.value)
        except ValueError as error:
            return dex.force_fail(str(error))
        intake_attribute.set(context, completed)
        return dex.graceful_complete(completed)


# dex:group group-id:airtable group-label:"Airtable"
# dex:explanation text:"Mark the record as deleted since the change and complete the Flow."
class RecordRemovedRecord(dex.Step[QueryResult[Record]]):
    """Record that the changed record is gone."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "RecordRemovedRecord"

    def execute(self, context: dex.Context, input: QueryResult[Record]) -> dex.StepDecision:
        """Complete as removed."""
        removed = replace(intake_attribute.get(context), is_removed=True, is_completed=True)
        intake_attribute.set(context, removed)
        return dex.graceful_complete(removed)


class RecordChangedIntakeFlow(dex.Flow[ChangedRecord]):
    """Record one changed Airtable record and read it back through the airtable-intake connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the connection named CONNECTION_NAME, the one whose table the Trigger polls."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("record-changed intake requires the static airtable-intake connection")
        self._record = RecordChangedRecord()
        self._complete = CompleteRecordIntake()
        self._removed = RecordRemovedRecord()
        self._read_back = new_get_record_step(
            step_type=_READ_BACK_STEP_TYPE,
            step_input_type=ChangedRecord,
            annotations={
                "groupId": "airtable",
                "groupLabel": "Airtable",
                "explanation": "Read the changed record back by its ID to capture its current fields.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_read_back_input,
            found=self._complete,
            not_found=self._removed,
        )

    def get_flow_type(self) -> str:
        """Return the stable Dex Flow identity, FLOW_TYPE."""
        return "AirtableRecordChangedIntakePython"

    def get_steps(self) -> dex.StepList[ChangedRecord]:
        """Record the change, read the record back, and complete with its current fields or as removed."""
        return dex.StepList.start_step(self._record).other_steps(
            self._read_back, self._complete, self._removed
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the intake Attribute."""
        return dex.PersistenceSchema.of(intake_attribute)

    # Declares the recordChanged binding and its pickers for Dex Web; Dex reads one static return.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_record_changed_trigger_binding(
                connection_name="airtable-intake",
                binding_name="intake-record-changed",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "intakeBase",
                            "basePicker",
                            "Base",
                            "Select the base that holds the table to watch; Load bases lists the bases the "
                            "personal access token can reach, and the picker stores the base ID and name. To "
                            "enter the ID instead, copy the app followed by 14 letters and digits from the "
                            "base's https://airtable.com/ address. It cannot be blank.",
                            True,
                            (
                                ConnectorUIBinding("baseId", "/baseId"),
                                ConnectorUIBinding("baseName", "/baseName"),
                            ),
                        ),
                        ConnectorUIUnit(
                            "intakeTable",
                            "tablePicker",
                            "Table",
                            "Select the table whose created and changed records start this Flow, after "
                            "saving the base; the picker stores the table ID, which keeps working after a "
                            "rename, and the table name. Records already in the table when the Worker first "
                            "polls never start Flows. It cannot be blank.",
                            True,
                            (
                                ConnectorUIBinding("baseId", "/baseId"),
                                ConnectorUIBinding("tableId", "/tableId"),
                                ConnectorUIBinding("tableName", "/tableName"),
                            ),
                        ),
                        ConnectorUIUnit(
                            "intakeLastModified",
                            "lastModifiedTimeFieldPicker",
                            "Last modified time field",
                            "Select the table's Last modified time field, after saving the table; the picker "
                            "stores the field ID and name. If the table has none, add a field of type Last "
                            "modified time with Include time turned on in Airtable. Its setting decides what "
                            "counts as a change: all fields, or only the fields it watches, so a Status-only "
                            "field starts a Flow only when Status changes. It cannot be blank.",
                            True,
                            (
                                ConnectorUIBinding("baseId", "/baseId"),
                                ConnectorUIBinding("tableId", "/tableId"),
                                ConnectorUIBinding("fieldId", "/fieldId"),
                                ConnectorUIBinding("fieldName", "/fieldName"),
                            ),
                        ),
                    )
                ),
            )
        ]
