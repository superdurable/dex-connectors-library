# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Pinecone vector upsert and query operations for Dex connector Steps."""

from .client import (
    API_VERSION,
    CONNECTOR_ID,
    DEFECT,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    QUERIED,
    QUERY_VECTORS_DEFINITION,
    UPSERT_VECTORS_DEFINITION,
    UPSERTED,
    Client,
    Config,
    Credentials,
    QueryVectorsOperation,
    UpsertVectorsOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import QueryVectorsInput, QueryVectorsOutput, UpsertVectorsInput, UpsertVectorsOutput, Vector
from .steps import new_query_vectors_step, new_upsert_vectors_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "QueryVectorsInput",
    "QueryVectorsOperation",
    "QueryVectorsOutput",
    "UpsertVectorsInput",
    "UpsertVectorsOperation",
    "UpsertVectorsOutput",
    "Vector",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "new_query_vectors_step",
    "new_upsert_vectors_step",
    "API_VERSION",
    "CONNECTOR_ID",
    "DEFECT",
    "INVALID_RESPONSE",
    "PROVIDER_REJECTED",
    "QUERIED",
    "QUERY_VECTORS_DEFINITION",
    "UPSERT_VECTORS_DEFINITION",
    "UPSERTED",
]
