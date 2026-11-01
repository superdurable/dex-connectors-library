# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Cohere scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..client import CONNECTOR_ID, EMBED_DEFINITION, EMBEDDED, PROVIDER_REJECTED, RANKED, RERANK_DEFINITION
from ..connection import Connection, new_connection_with_operations
from ..models import EmbedInput, EmbedOutput, RankedDocument, RerankInput, RerankOutput


def embed_default_embedding() -> Case[EmbedOutput]:
    """Return the manifest's embed defaultEmbedding answer."""
    return Case.branch_result(EMBEDDED, EmbedOutput("embed-mock-1", ((0.1, 0.2, 0.3),)))


def embed_invalid_key() -> Case[EmbedOutput]:
    """Return the manifest's embed invalidKey answer."""
    return Case.branch_result(
        PROVIDER_REJECTED,
        EmbedOutput(),
        Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "embed", "Cohere rejected the API key."),
    )


def rerank_default_ranking() -> Case[RerankOutput]:
    """Return the manifest's rerank defaultRanking answer."""
    return Case.branch_result(RANKED, RerankOutput((RankedDocument(0, 0.92), RankedDocument(1, 0.08))))


def rerank_invalid_key() -> Case[RerankOutput]:
    """Return the manifest's rerank invalidKey answer."""
    return Case.branch_result(
        PROVIDER_REJECTED,
        RerankOutput(),
        Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "rerank", "Cohere rejected the API key."),
    )


class Mock:
    """Script Cohere Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "cohere") -> None:
        self.embed_script: Query[EmbedInput, EmbedOutput] = Query(EMBED_DEFINITION)
        self.embed_script.default(embed_default_embedding())
        self.rerank_script: Query[RerankInput, RerankOutput] = Query(RERANK_DEFINITION)
        self.rerank_script.default(rerank_default_ranking())
        self._connection = new_connection_with_operations(
            self.embed_script, self.rerank_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "embed_default_embedding",
    "embed_invalid_key",
    "rerank_default_ranking",
    "rerank_invalid_key",
]
