# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Cohere embedding and rerank Queries for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    DEFECT,
    EMBED_DEFINITION,
    EMBEDDED,
    INPUT_TYPES,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    RANKED,
    RERANK_DEFINITION,
    Client,
    Config,
    Credentials,
    EmbedOperation,
    RerankOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import EmbedInput, EmbedOutput, RankedDocument, RerankInput, RerankOutput
from .steps import new_embed_step, new_rerank_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "EmbedInput",
    "EmbedOperation",
    "EmbedOutput",
    "RankedDocument",
    "RerankInput",
    "RerankOperation",
    "RerankOutput",
    "new_connection",
    "new_connection_with_operations",
    "new_embed_step",
    "new_project_connection",
    "new_rerank_step",
    "CONNECTOR_ID",
    "DEFECT",
    "EMBED_DEFINITION",
    "EMBEDDED",
    "INPUT_TYPES",
    "INVALID_RESPONSE",
    "PROVIDER_REJECTED",
    "RANKED",
    "RERANK_DEFINITION",
]
