# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable PandaDoc document inspection Flow using getDocument."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.pandadoc import (
    Connection,
    DocumentStatus,
    GetDocumentInput,
    new_get_document_step,
)

FLOW_TYPE = "PandaDocDocumentInspectionPython"
CONNECTION_NAME = "pandadoc"
_CALL_STEP_TYPE = "CallPandaDoc"
_RESULT = dex.Attribute("pandadoc-document", DocumentStatus)


def map_to_operation_input(request: GetDocumentInput) -> GetDocumentInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:pandadoc group-label:"PandaDoc"
# dex:explanation text:"Start the PandaDoc request."
class _StartRequest(dex.Step[GetDocumentInput]):
    def get_step_type(self) -> str:
        return "StartPandaDocRequest"

    def execute(self, context: dex.Context, input: GetDocumentInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallPandaDoc"), input)


# dex:group group-id:pandadoc group-label:"PandaDoc"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[DocumentStatus]]):
    def get_step_type(self) -> str:
        return "CompletePandaDocRequest"

    def execute(self, context: dex.Context, input: QueryResult[DocumentStatus]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DocumentInspectionFlow(dex.Flow[GetDocumentInput]):
    """Read one document through the static pandadoc connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("document inspection requires the static pandadoc connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_document_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetDocumentInput,
            annotations={
                "groupId": "pandadoc",
                "groupLabel": "PandaDoc",
                "explanation": "Read one document from the connected PandaDoc account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "PandaDocDocumentInspectionPython"

    def get_steps(self) -> dex.StepList[GetDocumentInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
