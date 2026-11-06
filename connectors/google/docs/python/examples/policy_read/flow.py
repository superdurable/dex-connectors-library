# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Google Doc through a Dex Flow."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.google_docs import (
    Connection,
    GetDocumentTextInput,
    GetDocumentTextOutput,
    new_get_document_text_step,
)

CONNECTION_NAME = "google-docs-policy"


@dataclass(frozen=True, slots=True)
class PolicyReadRequest:
    """Identify the Google Doc whose first tab should be read."""

    document_id: str = field(default="", metadata={"dex_json_name": "documentId"})


# dex:group group-id:google-docs group-label:"Google Docs"
# dex:explanation text:"Complete with the bounded text returned by Google Docs."
class _CompletePolicyRead(dex.Step[QueryResult[GetDocumentTextOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGoogleDocsPolicyRead"

    def execute(
        self, context: dex.Context, input: QueryResult[GetDocumentTextOutput]
    ) -> dex.StepDecision:
        return dex.graceful_complete(input.value)


class PolicyReadFlow(dex.Flow[PolicyReadRequest]):
    """Read one policy document through the static Google Docs connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("policy read requires the static Google Docs connection")
        self._complete = _CompletePolicyRead()
        self._read = new_get_document_text_step(
            step_type="ReadGoogleDocsPolicy",
            step_input_type=PolicyReadRequest,
            annotations={
                "groupId": "google-docs",
                "groupLabel": "Google Docs",
                "explanation": "Read the first tab as plain text with pending suggestions rejected.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: GetDocumentTextInput(
                document_id=request.document_id, format="plainText"
            ),
            read=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "GoogleDocsPolicyRead"

    def get_steps(self) -> dex.StepList[PolicyReadRequest]:
        """Register the read and completion Steps."""
        return dex.StepList.start_step(self._read).other_steps(self._complete)
