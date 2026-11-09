# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Ensure one OpenSearch document ID is absent."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.opensearch import (
    Connection,
    DeletedDocument,
    DeleteDocumentInput,
    new_delete_document_step,
)

CONNECTION_NAME = "opensearch"
_OUTCOME = dex.Attribute("opensearch-python-deleted-document", DeletedDocument)


def map_to_operation_input(request: DeleteDocumentInput) -> DeleteDocumentInput:
    """Forward the typed non-secret Flow input."""
    return request


# dex:group group-id:document-delete group-label:"Document delete"
# dex:explanation text:"Start the OpenSearch provider operation."
class _Start(dex.Step[DeleteDocumentInput]):
    def get_step_type(self) -> str:
        return "StartDeleteOpenSearchDocument"

    def execute(self, context: dex.Context, input: DeleteDocumentInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("DeleteOpenSearchDocument"), input)


# dex:group group-id:document-delete group-label:"Document delete"
# dex:explanation text:"Persist the confirmed OpenSearch result and complete the Flow."
class _Complete(dex.Step[MutationResult[DeletedDocument]]):
    def get_step_type(self) -> str:
        return "CompleteOpenSearchDelete"

    def execute(self, context: dex.Context, input: MutationResult[DeletedDocument]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class DocumentDeleteFlow(dex.Flow[DeleteDocumentInput]):
    """Ensure one OpenSearch document ID is absent."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("DocumentDeleteFlow requires the static opensearch connection")
        self._start = _Start()
        self._complete = _Complete()
        self._provider = new_delete_document_step(
            step_type="DeleteOpenSearchDocument",
            step_input_type=DeleteDocumentInput,
            annotations={
                "groupId": "document-delete",
                "groupLabel": "Document delete",
                "explanation": "Ensure one OpenSearch document ID is absent.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            absent=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable OpenSearch Flow identity."""
        return "OpenSearchPythonDocumentDeleteFlow"

    def get_steps(self) -> dex.StepList[DeleteDocumentInput]:
        """Register start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._provider, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist only the provider result for this Flow."""
        return dex.PersistenceSchema.of(_OUTCOME)
