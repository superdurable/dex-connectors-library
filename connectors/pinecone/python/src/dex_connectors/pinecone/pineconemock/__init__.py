# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Pinecone scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..client import CONNECTOR_ID, QUERIED, QUERY_VECTORS_DEFINITION, UPSERT_VECTORS_DEFINITION, UPSERTED
from ..connection import Connection, new_connection_with_operations
from ..models import QueryVectorsInput, QueryVectorsOutput, UpsertVectorsInput, UpsertVectorsOutput


def upsert_vectors_upserted(
    value: UpsertVectorsOutput, failure: Failure | None = None
) -> Case[UpsertVectorsOutput]:
    """Script the upserted route."""
    return Case.branch_result(UPSERTED, value, failure)


def upsert_vectors_default_answer() -> Case[UpsertVectorsOutput]:
    """Return the manifest's upsertVectors default answer."""
    return upsert_vectors_upserted(UpsertVectorsOutput(1))


def query_vectors_queried(
    value: QueryVectorsOutput, failure: Failure | None = None
) -> Case[QueryVectorsOutput]:
    """Script the queried route."""
    return Case.branch_result(QUERIED, value, failure)


def query_vectors_default_answer() -> Case[QueryVectorsOutput]:
    """Return the manifest's queryVectors default answer."""
    return query_vectors_queried(
        QueryVectorsOutput({"matches": [{"id": "vec-meridian", "score": 0.98}], "namespace": "example"})
    )


class Mock:
    """Script Pinecone operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "pinecone") -> None:
        self.upsert_vectors_script: Mutation[UpsertVectorsInput, UpsertVectorsOutput] = Mutation(
            UPSERT_VECTORS_DEFINITION
        )
        self.upsert_vectors_script.default(upsert_vectors_default_answer())
        self.query_vectors_script: Query[QueryVectorsInput, QueryVectorsOutput] = Query(
            QUERY_VECTORS_DEFINITION
        )
        self.query_vectors_script.default(query_vectors_default_answer())
        self._connection = new_connection_with_operations(
            self.upsert_vectors_script,
            self.query_vectors_script,
            ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "upsert_vectors_upserted",
    "upsert_vectors_default_answer",
    "query_vectors_queried",
    "query_vectors_default_answer",
]
