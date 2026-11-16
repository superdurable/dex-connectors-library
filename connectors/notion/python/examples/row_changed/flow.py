# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Every row created or edited in the picked Notion data source starts one Flow, which reads the row back
and records its current properties."""

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

from dex_connectors.notion import (
    Connection,
    DatabaseRowChangedEvent,
    DatabaseRowChangeKind,
    GetPageInput,
    GetPageOutput,
    Page,
    define_database_row_changed_trigger_binding,
    new_get_page_step,
)

CONNECTION_NAME = "notion-row-watcher"
DATABASE_ROW_CHANGED_TRIGGER_BINDING = "database-row-changes"
FLOW_ID_PREFIX = "notion-row-change-"


@dataclass(frozen=True, slots=True)
class RowChangeInput:
    """One databaseRowChanged delivery: the stable Trigger event ID beside the delivered change."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    change: DatabaseRowChangedEvent


@dataclass(frozen=True, slots=True)
class ChangeOutcome:
    """The Flow result and the value of its outcome Attribute."""

    data_source_id: str = field(metadata={"dex_json_name": "dataSourceId"})
    row_id: str = field(metadata={"dex_json_name": "rowId"})
    change_kind: str = field(metadata={"dex_json_name": "changeKind"})
    current: Page
    """The row read back from Notion."""


row_change_attribute = dex.Attribute("notion-row-change", RowChangeInput)
row_change_outcome_attribute = dex.Attribute("notion-row-change-outcome", ChangeOutcome)


def resolve_flow_id(event: TriggerEvent[DatabaseRowChangedEvent]) -> str:
    """Name one Flow per row version; a redelivered change finds its existing Flow."""
    return FLOW_ID_PREFIX + event.id


def map_to_flow_input(event: TriggerEvent[DatabaseRowChangedEvent]) -> RowChangeInput:
    """Keep the Trigger event ID beside the delivered change."""
    return RowChangeInput(trigger_event_id=event.id, change=event.payload)


def accept_every_row_change(event: TriggerEvent[DatabaseRowChangedEvent]) -> bool:
    """Admit every change; an application narrows it, for example to created rows."""
    return True


def map_to_get_page_input(input: RowChangeInput) -> GetPageInput:
    """Read the changed row's properties without its body blocks."""
    return GetPageInput(page_id=input.change.row.id, skip_content=True)


def validate_row_change_input(input: RowChangeInput) -> str:
    """Return why the delivered change cannot be recorded, or an empty string."""
    if not input.trigger_event_id.strip():
        return "triggerEventId is required"
    if not input.change.data_source_id.strip() or not input.change.row.id.strip():
        return "change.dataSourceId and change.row.id are required"
    if input.change.change_kind not in {kind.value for kind in DatabaseRowChangeKind}:
        return "change.changeKind must be created or updated"
    return ""


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the delivered row change, then read the row back from Notion."
class RecordRowChange(dex.Step[RowChangeInput]):
    """Store the delivered change before the row is read back."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "RecordRowChange"

    def execute(self, context: dex.Context, input: RowChangeInput) -> dex.StepDecision:
        """Fail an invalid delivery; otherwise record it and read the row."""
        problem = validate_row_change_input(input)
        if problem:
            return dex.force_fail(problem)
        row_change_attribute.set(context, input)
        return dex.go_to(dex.StepRef("ReadChangedRow"), input)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the row read back from Notion as the outcome and complete the Flow."
class CompleteRowChange(dex.Step[QueryResult[GetPageOutput]]):
    """Complete with the change and the row's current properties."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "CompleteRowChange"

    def execute(self, context: dex.Context, input: QueryResult[GetPageOutput]) -> dex.StepDecision:
        """Save and return the outcome."""
        change = row_change_attribute.get(context).change
        outcome = ChangeOutcome(change.data_source_id, change.row.id, change.change_kind, input.value.page)
        row_change_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


class DatabaseRowChangeFlow(dex.Flow[RowChangeInput]):
    """Record one Notion database row change, reading the row back."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Notion connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("row change Flow requires the notion-row-watcher connection")
        self.record = RecordRowChange()
        self.complete = CompleteRowChange()
        self.read_row = new_get_page_step(
            step_type="ReadChangedRow",
            step_input_type=RowChangeInput,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the changed row back, so the record holds its current properties "
                "rather than the polled snapshot.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_page_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Dex Flow identity."""
        return "NotionDatabaseRowChangePython"

    def get_steps(self) -> dex.StepList[RowChangeInput]:
        """Record the change, read the row back, and complete."""
        return dex.StepList.start_step(self.record).other_steps(self.read_row, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(row_change_attribute, row_change_outcome_attribute)

    # Declares the databaseRowChanged binding and its data source picker; Dex reads one static return.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_database_row_changed_trigger_binding(
                connection_name="notion-row-watcher",
                binding_name="database-row-changes",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "watchedDataSource",
                            "dataSourcePicker",
                            "Watched database",
                            "Choose the Notion data source whose created and edited rows start this Flow: "
                            "paste the database link from its ••• menu > Copy link at https://app.notion.com "
                            "and pick a data source, or paste a data source ID from the database's settings "
                            "> Manage data sources > Copy data source ID. Share the database with this "
                            "connection first. The picker saves the data source ID and name; the binding "
                            "needs one, so the Worker does not start until it is saved. The first start "
                            "records existing rows as a baseline and starts no Flow for them. Restart the "
                            "Worker after saving; a changed data source records a new baseline.",
                            True,
                            (
                                ConnectorUIBinding("dataSourceId", "/dataSourceId"),
                                ConnectorUIBinding("dataSourceName", "/dataSourceName"),
                            ),
                        ),
                    )
                ),
            )
        ]
