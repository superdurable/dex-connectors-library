# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Search one OpenSearch index with a bounded page."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.opensearch import Connection, SearchDocumentsInput, SearchPage, new_search_documents_step

CONNECTION_NAME = "opensearch"
_OUTCOME = dex.Attribute("opensearch-python-search-page", SearchPage)


def map_to_operation_input(request: SearchDocumentsInput) -> SearchDocumentsInput:
    """Forward the typed non-secret Flow input."""
    return request


# dex:group group-id:document-search group-label:"Document search"
# dex:explanation text:"Start the OpenSearch provider operation."
class _Start(dex.Step[SearchDocumentsInput]):
    def get_step_type(self) -> str:
        return "StartSearchOpenSearchDocuments"

    def execute(self, context: dex.Context, input: SearchDocumentsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("SearchOpenSearchDocuments"), input)


# dex:group group-id:document-search group-label:"Document search"
# dex:explanation text:"Persist the confirmed OpenSearch result and complete the Flow."
class _Complete(dex.Step[QueryResult[SearchPage]]):
    def get_step_type(self) -> str:
        return "CompleteOpenSearchSearch"

    def execute(self, context: dex.Context, input: QueryResult[SearchPage]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class DocumentSearchFlow(dex.Flow[SearchDocumentsInput]):
    """Search one OpenSearch index with a bounded page."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("DocumentSearchFlow requires the static opensearch connection")
        self._start = _Start()
        self._complete = _Complete()
        self._provider = new_search_documents_step(
            step_type="SearchOpenSearchDocuments",
            step_input_type=SearchDocumentsInput,
            annotations={
                "groupId": "document-search",
                "groupLabel": "Document search",
                "explanation": "Search one OpenSearch index with a bounded page.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable OpenSearch Flow identity."""
        return "OpenSearchPythonDocumentSearchFlow"

    def get_steps(self) -> dex.StepList[SearchDocumentsInput]:
        """Register start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._provider, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist only the provider result for this Flow."""
        return dex.PersistenceSchema.of(_OUTCOME)
