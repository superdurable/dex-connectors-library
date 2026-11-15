# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A Flow, started by rowAdded, that records and reads back one row appended to a Google Sheets tab."""

from dataclasses import dataclass, field
from datetime import datetime

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    QueryResult,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.google_spreadsheet import (
    UI_SHEET_TAB_PICKER_PORT_SPREADSHEET_ID,
    UI_SHEET_TAB_PICKER_PORT_TAB,
    UI_SPREADSHEET_PICKER_PORT_SPREADSHEET_ID,
    UI_UNIT_SHEET_TAB_PICKER,
    UI_UNIT_SPREADSHEET_PICKER,
    Connection,
    GetValuesInput,
    GetValuesOutput,
    RowAddedEvent,
    define_row_added_trigger_binding,
    new_get_values_step,
)

FLOW_TYPE = "GoogleSheetsNewRowIntakePython"
CONNECTION_NAME = "google-sheets-intake"
"""The static Dex Web connection whose tab the Trigger polls."""
NEW_ROW_TRIGGER_BINDING = "new-intake-row"
"""The rowAdded binding whose rows start this Flow."""
FLOW_ID_PREFIX = "google-sheets-row-"
"""Precedes the Trigger event ID in every Flow ID, so a redelivered row maps to one Flow."""
READ_ADDED_ROW_STEP_TYPE = "ReadAddedRow"


@dataclass(frozen=True, slots=True)
class AddedRow:
    """The Flow's start input: one row the rowAdded Trigger found."""

    event_id: str = field(metadata={"dex_json_name": "eventId"})
    spreadsheet_id: str = field(metadata={"dex_json_name": "spreadsheetId"})
    sheet_name: str = field(metadata={"dex_json_name": "sheetName"})
    row_number: int = field(metadata={"dex_json_name": "rowNumber"})
    values: dict[str, str]
    detected_at: datetime = field(metadata={"dex_json_name": "detectedAt"})


def accept_added_row(event: TriggerEvent[RowAddedEvent]) -> bool:
    """The application's admission rule: every row the Trigger reports starts a Flow."""
    return True


def resolve_flow_id(event: TriggerEvent[RowAddedEvent]) -> str:
    """Derive the Flow ID from the event ID, so every redelivery of a row maps to one Flow."""
    return FLOW_ID_PREFIX + event.id


def map_to_flow_input(event: TriggerEvent[RowAddedEvent]) -> AddedRow:
    """Copy the added row into the Flow's start input."""
    row = event.payload
    return AddedRow(
        event_id=event.id,
        spreadsheet_id=row.spreadsheet_id,
        sheet_name=row.sheet_name,
        row_number=row.row_number,
        values=dict(row.values or {}),
        detected_at=event.occurred_at,
    )


@dataclass(frozen=True, slots=True)
class Outcome:
    """The durable result of one intake and the Flow's completion output."""

    row: AddedRow
    """The row the Trigger reported."""
    current_cells: tuple[str, ...] | None = field(default=None, metadata={"dex_json_name": "currentCells"})
    """The row's formatted cells read back from Google Sheets when the Flow ran; they differ from ``row``
    when the row was edited, or rows above it moved, after the poll. None until read."""


def map_to_get_values_input(row: AddedRow) -> GetValuesInput:
    """Read the reported row of the polled tab as one whole-row A1 range, such as 'Leads'!7:7.

    A single quote in the tab title is doubled, as A1 notation requires.
    """
    row_number = str(row.row_number)
    quoted_tab = "'" + row.sheet_name.replace("'", "''") + "'"
    return GetValuesInput(row.spreadsheet_id, f"{quoted_tab}!{row_number}:{row_number}")


added_row_attribute = dex.Attribute("google-sheets-added-row", Outcome)


# dex:group group-id:intake group-label:"Intake"
# dex:explanation text:"Persist the added spreadsheet row the rowAdded Trigger reported."
class RecordAddedRow(dex.Step[AddedRow]):
    """Persist the header-keyed row, then read it back."""

    def get_step_type(self) -> str:
        """Keep the Step identity stable across Worker versions."""
        return "RecordAddedRow"

    def execute(self, context: dex.Context, input: AddedRow) -> dex.StepDecision:
        """Save the row for Dex Web, or fail a row without its identity."""
        if not input.event_id or not input.spreadsheet_id or input.row_number < 2:
            return dex.force_fail(
                "an added row requires its event ID, spreadsheet ID, and a row below the header"
            )
        added_row_attribute.set(context, Outcome(input))
        return dex.go_to(dex.StepRef("ReadAddedRow"), input)


# dex:group group-id:intake-complete group-label:"Complete intake"
# dex:explanation text:"Record the row's current cells and complete the Flow."
class CompleteRowIntake(dex.Step[QueryResult[GetValuesOutput]]):
    """Store the cells read back beside the reported row and complete."""

    def get_step_type(self) -> str:
        """Keep the Step identity stable across Worker versions."""
        return "CompleteRowIntake"

    def execute(self, context: dex.Context, input: QueryResult[GetValuesOutput]) -> dex.StepDecision:
        """Sheets omits values for a row that is now empty, so the read-back is then an empty cell list."""
        values = input.value.values or ()
        outcome = Outcome(added_row_attribute.get(context).row, tuple(values[0] or ()) if values else ())
        added_row_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


class NewRowIntakeFlow(dex.Flow[AddedRow]):
    """Record one appended spreadsheet row and read it back; an application would create a lead here."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Google Sheets connection the read-back Step uses."""
        if connection is None:
            raise ValueError("Google Sheets connection is required")
        self.record = RecordAddedRow()
        self.complete = CompleteRowIntake()
        self.read = new_get_values_step(
            step_type=READ_ADDED_ROW_STEP_TYPE,
            step_input_type=AddedRow,
            annotations={
                "groupId": "google-sheets",
                "groupLabel": "Google Sheets",
                "explanation": "Read the added row's current cells back from the tab.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_values_input,
            read=self.complete,
        )

    def get_flow_type(self) -> str:
        """Keep the application Flow identity stable across Worker versions."""
        return "GoogleSheetsNewRowIntakePython"

    def get_steps(self) -> dex.StepList[AddedRow]:
        """Record the row, read it back from Google Sheets, and complete."""
        return dex.StepList.start_step(self.record).other_steps(self.read, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the intake outcome Attribute."""
        return dex.PersistenceSchema.of(added_row_attribute)

    # Dex Web configures this static binding with pickers; the analyzer requires a lone return statement.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_row_added_trigger_binding(
                connection_name="google-sheets-intake",
                binding_name="new-intake-row",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "spreadsheet",
                            UI_UNIT_SPREADSHEET_PICKER,
                            "Spreadsheet",
                            "Select the spreadsheet whose new rows start this Flow. Choose spreadsheet "
                            "opens the Google Picker, which grants this app the drive.file access the "
                            "Trigger polls with, and the unit stores the spreadsheet ID. To enter the ID "
                            "instead, copy the non-secret text between /d/ and /edit in the spreadsheet's "
                            "https://docs.google.com/spreadsheets/d/ address; it works only for a "
                            "spreadsheet already granted to this app. It cannot be blank.",
                            True,
                            (
                                ConnectorUIBinding(
                                    UI_SPREADSHEET_PICKER_PORT_SPREADSHEET_ID, "/spreadsheetId"
                                ),
                            ),
                        ),
                        ConnectorUIUnit(
                            "tab",
                            UI_UNIT_SHEET_TAB_PICKER,
                            "Tab",
                            "Select the tab to watch; the picker loads the chosen spreadsheet's current tab "
                            "titles and stores the exact title. Row 1 must hold the column headers. Rows "
                            "already on the tab when the Worker first polls never start Flows, and renaming "
                            "the tab later stops the Trigger until you pick it again. It cannot be blank.",
                            True,
                            (
                                ConnectorUIBinding(UI_SHEET_TAB_PICKER_PORT_SPREADSHEET_ID, "/spreadsheetId"),
                                ConnectorUIBinding(UI_SHEET_TAB_PICKER_PORT_TAB, "/sheetName"),
                            ),
                        ),
                    )
                ),
            )
        ]
