# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Lucid document discovery Flow using listDocuments."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.lucid import (
    Connection,
    DocumentsPage,
    ListDocumentsInput,
    new_list_documents_step,
)

FLOW_TYPE = "LucidDocumentDiscoveryPython"
CONNECTION_NAME = "lucid"
_CALL_STEP_TYPE = "CallLucid"
_RESULT = dex.Attribute("lucid-documents", DocumentsPage)


def map_to_operation_input(request: ListDocumentsInput) -> ListDocumentsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:lucid group-label:"Lucid"
# dex:explanation text:"Start the Lucid request."
class _StartRequest(dex.Step[ListDocumentsInput]):
    def get_step_type(self) -> str:
        return "StartLucidRequest"

    def execute(self, context: dex.Context, input: ListDocumentsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallLucid"), input)


# dex:group group-id:lucid group-label:"Lucid"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[DocumentsPage]]):
    def get_step_type(self) -> str:
        return "CompleteLucidRequest"

    def execute(self, context: dex.Context, input: QueryResult[DocumentsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DocumentDiscoveryFlow(dex.Flow[ListDocumentsInput]):
    """Read one document page through the static lucid connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("document discovery requires the static lucid connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_documents_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListDocumentsInput,
            annotations={
                "groupId": "lucid",
                "groupLabel": "Lucid",
                "explanation": "Read one document page from the connected Lucid account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "LucidDocumentDiscoveryPython"

    def get_steps(self) -> dex.StepList[ListDocumentsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
