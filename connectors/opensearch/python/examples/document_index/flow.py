# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replace one OpenSearch document under a stable ID."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.opensearch import Connection, IndexDocumentInput, IndexedDocument, new_index_document_step

CONNECTION_NAME = "opensearch"
_OUTCOME = dex.Attribute("opensearch-python-indexed-document", IndexedDocument)


def map_to_operation_input(request: IndexDocumentInput) -> IndexDocumentInput:
    """Forward the typed non-secret Flow input."""
    return request


# dex:group group-id:document-index group-label:"Document index"
# dex:explanation text:"Start the OpenSearch provider operation."
class _Start(dex.Step[IndexDocumentInput]):
    def get_step_type(self) -> str:
        return "StartIndexOpenSearchDocument"

    def execute(self, context: dex.Context, input: IndexDocumentInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("IndexOpenSearchDocument"), input)


# dex:group group-id:document-index group-label:"Document index"
# dex:explanation text:"Persist the confirmed OpenSearch result and complete the Flow."
class _Complete(dex.Step[MutationResult[IndexedDocument]]):
    def get_step_type(self) -> str:
        return "CompleteOpenSearchIndex"

    def execute(self, context: dex.Context, input: MutationResult[IndexedDocument]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class DocumentIndexFlow(dex.Flow[IndexDocumentInput]):
    """Replace one OpenSearch document under a stable ID."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("DocumentIndexFlow requires the static opensearch connection")
        self._start = _Start()
        self._complete = _Complete()
        self._provider = new_index_document_step(
            step_type="IndexOpenSearchDocument",
            step_input_type=IndexDocumentInput,
            annotations={
                "groupId": "document-index",
                "groupLabel": "Document index",
                "explanation": "Replace one OpenSearch document under a stable ID.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            indexed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable OpenSearch Flow identity."""
        return "OpenSearchPythonDocumentIndexFlow"

    def get_steps(self) -> dex.StepList[IndexDocumentInput]:
        """Register start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._provider, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist only the provider result for this Flow."""
        return dex.PersistenceSchema.of(_OUTCOME)
