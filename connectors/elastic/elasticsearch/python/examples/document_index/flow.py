# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replace one Elasticsearch document under a stable ID."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.elastic_elasticsearch import (
    Connection,
    IndexDocumentInput,
    IndexedDocument,
    new_index_document_step,
)

CONNECTION_NAME = "elasticsearch"
_OUTCOME = dex.Attribute("elasticsearch-python-indexed-document", IndexedDocument)


def map_to_operation_input(request: IndexDocumentInput) -> IndexDocumentInput:
    """Forward the typed non-secret Flow input."""
    return request


# dex:group group-id:document-index group-label:"Document index"
# dex:explanation text:"Start the Elasticsearch provider operation."
class _Start(dex.Step[IndexDocumentInput]):
    def get_step_type(self) -> str:
        return "StartIndexElasticsearchDocument"

    def execute(self, context: dex.Context, input: IndexDocumentInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("IndexElasticsearchDocument"), input)


# dex:group group-id:document-index group-label:"Document index"
# dex:explanation text:"Persist the confirmed Elasticsearch result and complete the Flow."
class _Complete(dex.Step[MutationResult[IndexedDocument]]):
    def get_step_type(self) -> str:
        return "CompleteElasticsearchIndex"

    def execute(self, context: dex.Context, input: MutationResult[IndexedDocument]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class DocumentIndexFlow(dex.Flow[IndexDocumentInput]):
    """Replace one Elasticsearch document under a stable ID."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("DocumentIndexFlow requires the static elasticsearch connection")
        self._start = _Start()
        self._complete = _Complete()
        self._provider = new_index_document_step(
            step_type="IndexElasticsearchDocument",
            step_input_type=IndexDocumentInput,
            annotations={
                "groupId": "document-index",
                "groupLabel": "Document index",
                "explanation": "Replace one Elasticsearch document under a stable ID.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            indexed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Elasticsearch Flow identity."""
        return "ElasticsearchPythonDocumentIndexFlow"

    def get_steps(self) -> dex.StepList[IndexDocumentInput]:
        """Register start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._provider, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist only the provider result for this Flow."""
        return dex.PersistenceSchema.of(_OUTCOME)
