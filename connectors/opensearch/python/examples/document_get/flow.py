# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one OpenSearch document by index and ID."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.opensearch import Connection, GetDocumentInput, RetrievedDocument, new_get_document_step

CONNECTION_NAME = "opensearch"
_OUTCOME = dex.Attribute("opensearch-python-retrieved-document", RetrievedDocument)


def map_to_operation_input(request: GetDocumentInput) -> GetDocumentInput:
    """Forward the typed non-secret Flow input."""
    return request


# dex:group group-id:document-get group-label:"Document retrieval"
# dex:explanation text:"Start the OpenSearch provider operation."
class _Start(dex.Step[GetDocumentInput]):
    def get_step_type(self) -> str:
        return "StartGetOpenSearchDocument"

    def execute(self, context: dex.Context, input: GetDocumentInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetOpenSearchDocument"), input)


# dex:group group-id:document-get group-label:"Document retrieval"
# dex:explanation text:"Persist the confirmed OpenSearch result and complete the Flow."
class _Complete(dex.Step[QueryResult[RetrievedDocument]]):
    def get_step_type(self) -> str:
        return "CompleteOpenSearchGet"

    def execute(self, context: dex.Context, input: QueryResult[RetrievedDocument]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class DocumentGetFlow(dex.Flow[GetDocumentInput]):
    """Read one OpenSearch document by index and ID."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("DocumentGetFlow requires the static opensearch connection")
        self._start = _Start()
        self._complete = _Complete()
        self._provider = new_get_document_step(
            step_type="GetOpenSearchDocument",
            step_input_type=GetDocumentInput,
            annotations={
                "groupId": "document-get",
                "groupLabel": "Document retrieval",
                "explanation": "Read one OpenSearch document by index and ID.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable OpenSearch Flow identity."""
        return "OpenSearchPythonDocumentGetFlow"

    def get_steps(self) -> dex.StepList[GetDocumentInput]:
        """Register start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._provider, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist only the provider result for this Flow."""
        return dex.PersistenceSchema.of(_OUTCOME)
