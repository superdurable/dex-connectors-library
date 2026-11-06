# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Upsert one contact through the Google Sheets connector and Dex."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.google_spreadsheet import Connection, UpsertRowInput, UpsertRowOutput, new_upsert_row_step

CONNECTION_NAME = "google-sheets-contacts"
FLOW_TYPE = "GoogleSheetsUpsertContactPython"


@dataclass(frozen=True, slots=True)
class ContactInput:
    """Spreadsheet, tab, and contact values supplied to the Flow."""

    spreadsheet_id: str = field(metadata={"dex_json_name": "spreadsheetId"})
    sheet_name: str = field(metadata={"dex_json_name": "sheetName"})
    email: str
    name: str
    status: str

    def to_operation_input(self) -> UpsertRowInput:
        """Map a contact to the stable email-key row Mutation."""
        return UpsertRowInput(
            self.spreadsheet_id,
            self.sheet_name,
            "email",
            self.email,
            {"email": self.email, "name": self.name, "status": self.status},
        )


contact_result = dex.Attribute("google-sheets-python-contact-result", UpsertRowOutput)


# dex:group group-id:google-sheets group-label:"Google Sheets"
# dex:explanation text:"Store the accepted contact row and complete the Flow."
class CompleteContact(dex.Step[MutationResult[UpsertRowOutput]]):
    """Persist the accepted row locator and complete the Flow."""

    def execute(self, context: dex.Context, input: MutationResult[UpsertRowOutput]) -> dex.StepDecision:
        """Store only the non-secret provider result."""
        contact_result.set(context, input.value)
        return dex.graceful_complete(input.value)


class ContactFlow(dex.Flow[ContactInput]):
    """Upsert a contact using one static Google Sheets connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the connector Mutation during Worker registration."""
        if connection is None:
            raise ValueError("Google Sheets connection is required")
        self.complete = CompleteContact()
        self.upsert = new_upsert_row_step(
            step_type="UpsertContactRow",
            step_input_type=ContactInput,
            annotations={
                "groupId": "google-sheets",
                "groupLabel": "Google Sheets",
                "explanation": "Upsert the contact in Google Sheets using email as the stable key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=ContactInput.to_operation_input,
            upserted=self.complete,
        )

    def get_flow_type(self) -> str:
        """Keep the Python example Flow identity stable."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ContactInput]:
        """Run the fenced provider Mutation and completion Step."""
        return dex.StepList.start_step(self.upsert).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable contact result Attribute."""
        return dex.PersistenceSchema.of(contact_result)
