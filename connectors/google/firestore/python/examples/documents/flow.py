# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Firestore page Flow using the listDocuments Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.google_firestore import (
    Connection,
    DocumentsPage,
    ListDocumentsInput,
    new_list_documents_step,
)

FLOW_TYPE = "FirestoreDocumentsListPython"
CONNECTION_NAME = "google-firestore"
_CALL_STEP_TYPE = "CallListDocuments"
_RESULT = dex.Attribute("google_firestore-python-page", DocumentsPage)


def map_to_operation_input(request: ListDocumentsInput) -> ListDocumentsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:google-firestore group-label:"Firestore"
# dex:explanation text:"Start the Firestore request."
class _StartRequest(dex.Step[ListDocumentsInput]):
    def get_step_type(self) -> str:
        return "StartListDocuments"

    def execute(self, context: dex.Context, input: ListDocumentsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListDocuments"), input)


# dex:group group-id:google-firestore group-label:"Firestore"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[DocumentsPage]]):
    def get_step_type(self) -> str:
        return "CompleteListDocuments"

    def execute(self, context: dex.Context, input: QueryResult[DocumentsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DocumentsFlow(dex.Flow[ListDocumentsInput]):
    """Run one listDocuments read through the static google_firestore connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the documents lookup Flow requires the static google_firestore connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_documents_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListDocumentsInput,
            annotations={
                "groupId": "google-firestore",
                "groupLabel": "Firestore",
                "explanation": "Read one Firestore page.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "FirestoreDocumentsListPython"

    def get_steps(self) -> dex.StepList[ListDocumentsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
