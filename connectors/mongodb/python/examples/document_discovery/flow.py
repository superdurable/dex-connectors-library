# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable MongoDB document discovery Flow with a static connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.mongodb import Connection, DocumentSet, FindDocumentsInput, new_find_documents_step

CONNECTION_NAME = "mongodb"
_RESULT = dex.Attribute("mongodb-python-documents", DocumentSet)


def map_to_operation_input(request: FindDocumentsInput) -> FindDocumentsInput:
    """Pass the typed filter to the provider Step."""
    return request


# dex:group group-id:mongodb group-label:"MongoDB"
# dex:explanation text:"Start the MongoDB document read."
class _Start(dex.Step[FindDocumentsInput]):
    def get_step_type(self) -> str:
        return "StartFindDocuments"

    def execute(self, context: dex.Context, input: FindDocumentsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallFindDocuments"), input)


# dex:group group-id:mongodb group-label:"MongoDB"
# dex:explanation text:"Store the read result and complete the Flow."
class _Complete(dex.Step[QueryResult[DocumentSet]]):
    def get_step_type(self) -> str:
        return "CompleteFindDocuments"

    def execute(self, context: dex.Context, input: QueryResult[DocumentSet]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DocumentDiscoveryFlow(dex.Flow[FindDocumentsInput]):
    """Find MongoDB documents and retain the bounded result in an Attribute."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("document discovery requires the mongodb connection")
        self._start = _Start()
        self._complete = _Complete()
        self._call = new_find_documents_step(
            step_type="CallFindDocuments",
            step_input_type=FindDocumentsInput,
            annotations={
                "groupId": "mongodb", "groupLabel": "MongoDB",
                "explanation": "Find documents in the selected MongoDB collection.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Python Flow identity."""
        return "MongoDBPythonDocumentDiscovery"

    def get_steps(self) -> dex.StepList[FindDocumentsInput]:
        """Register the start, Query, and result Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the bounded document set."""
        return dex.PersistenceSchema.of(_RESULT)
