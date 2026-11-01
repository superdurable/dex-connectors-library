# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated manifest-backed provider outcomes and scripts."""

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import QueryVectorsInput, QueryVectorsOutput, UpsertVectorsInput, UpsertVectorsOutput, Vector
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, PROVIDER_REJECTED, QUERIED, QUERY_VECTORS_DEFINITION, UPSERTED, UPSERT_VECTORS_DEFINITION

def upsert_vectors_upserted(value: UpsertVectorsOutput, failure: Failure | None = None) -> Case[UpsertVectorsOutput]:
    """Script the upserted branch."""
    return Case.branch_result(UPSERTED, value, failure)

setattr(upsert_vectors_upserted, '__module__', "dex_connectors.pinecone.pineconemock")

def upsert_vectors_provider_rejected(value: UpsertVectorsOutput, failure: Failure | None = None) -> Case[UpsertVectorsOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(upsert_vectors_provider_rejected, '__module__', "dex_connectors.pinecone.pineconemock")

def upsert_vectors_invalid_response(value: UpsertVectorsOutput, failure: Failure | None = None) -> Case[UpsertVectorsOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(upsert_vectors_invalid_response, '__module__', "dex_connectors.pinecone.pineconemock")

def upsert_vectors_defect(value: UpsertVectorsOutput, failure: Failure | None = None) -> Case[UpsertVectorsOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(upsert_vectors_defect, '__module__', "dex_connectors.pinecone.pineconemock")

def upsert_vectors_retry(failure: Failure, after_seconds: float = 0) -> Case[UpsertVectorsOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(upsert_vectors_retry, '__module__', "dex_connectors.pinecone.pineconemock")

def upsert_vectors_default_upsert_vectors() -> Case[UpsertVectorsOutput]:
    """Return the manifest defaultUpsertVectors outcome."""
    return Case.branch_result(UPSERTED, UpsertVectorsOutput(upserted_count=1))

setattr(upsert_vectors_default_upsert_vectors, '__module__', "dex_connectors.pinecone.pineconemock")

def query_vectors_queried(value: QueryVectorsOutput, failure: Failure | None = None) -> Case[QueryVectorsOutput]:
    """Script the queried branch."""
    return Case.branch_result(QUERIED, value, failure)

setattr(query_vectors_queried, '__module__', "dex_connectors.pinecone.pineconemock")

def query_vectors_provider_rejected(value: QueryVectorsOutput, failure: Failure | None = None) -> Case[QueryVectorsOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(query_vectors_provider_rejected, '__module__', "dex_connectors.pinecone.pineconemock")

def query_vectors_invalid_response(value: QueryVectorsOutput, failure: Failure | None = None) -> Case[QueryVectorsOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(query_vectors_invalid_response, '__module__', "dex_connectors.pinecone.pineconemock")

def query_vectors_defect(value: QueryVectorsOutput, failure: Failure | None = None) -> Case[QueryVectorsOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(query_vectors_defect, '__module__', "dex_connectors.pinecone.pineconemock")

def query_vectors_retry(failure: Failure, after_seconds: float = 0) -> Case[QueryVectorsOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(query_vectors_retry, '__module__', "dex_connectors.pinecone.pineconemock")

def query_vectors_default_query_vectors() -> Case[QueryVectorsOutput]:
    """Return the manifest defaultQueryVectors outcome."""
    return Case.branch_result(QUERIED, QueryVectorsOutput(data={'matches': [{'id': 'vec-meridian', 'score': 0.98}], 'namespace': 'example'}))

setattr(query_vectors_default_query_vectors, '__module__', "dex_connectors.pinecone.pineconemock")


class Mock:
    """Script manifest outcomes for this connector."""

    def __init__(self, connection_name: str = "pinecone") -> None:
        self.upsert_vectors_script: Mutation[UpsertVectorsInput, UpsertVectorsOutput] = Mutation(UPSERT_VECTORS_DEFINITION)
        self.upsert_vectors_script.default(upsert_vectors_default_upsert_vectors())
        self.query_vectors_script: Query[QueryVectorsInput, QueryVectorsOutput] = Query(QUERY_VECTORS_DEFINITION)
        self.query_vectors_script.default(query_vectors_default_query_vectors())
        self._connection = new_connection_with_operations(
            upsert_vectors=self.upsert_vectors_script,
            query_vectors=self.query_vectors_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock connection for Dex Step registration."""
        return self._connection

setattr(Mock, '__module__', "dex_connectors.pinecone.pineconemock")

__all__ = [
    "Mock",
    "upsert_vectors_upserted",
    "upsert_vectors_provider_rejected",
    "upsert_vectors_invalid_response",
    "upsert_vectors_defect",
    "upsert_vectors_retry",
    "upsert_vectors_default_upsert_vectors",
    "query_vectors_queried",
    "query_vectors_provider_rejected",
    "query_vectors_invalid_response",
    "query_vectors_defect",
    "query_vectors_retry",
    "query_vectors_default_query_vectors",
]
