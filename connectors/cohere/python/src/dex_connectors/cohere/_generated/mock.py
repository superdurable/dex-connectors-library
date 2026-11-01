# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated manifest-backed provider outcomes and scripts."""

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import EmbedInput, EmbedOutput, RankedDocument, RerankInput, RerankOutput
from .definitions import CONNECTOR_ID, DEFECT, EMBEDDED, EMBED_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED, RANKED, RERANK_DEFINITION

def embed_embedded(value: EmbedOutput, failure: Failure | None = None) -> Case[EmbedOutput]:
    """Script the embedded branch."""
    return Case.branch_result(EMBEDDED, value, failure)

setattr(embed_embedded, '__module__', "dex_connectors.cohere.coheremock")

def embed_provider_rejected(value: EmbedOutput, failure: Failure | None = None) -> Case[EmbedOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(embed_provider_rejected, '__module__', "dex_connectors.cohere.coheremock")

def embed_invalid_response(value: EmbedOutput, failure: Failure | None = None) -> Case[EmbedOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(embed_invalid_response, '__module__', "dex_connectors.cohere.coheremock")

def embed_defect(value: EmbedOutput, failure: Failure | None = None) -> Case[EmbedOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(embed_defect, '__module__', "dex_connectors.cohere.coheremock")

def embed_retry(failure: Failure, after_seconds: float = 0) -> Case[EmbedOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(embed_retry, '__module__', "dex_connectors.cohere.coheremock")

def embed_default_embedding() -> Case[EmbedOutput]:
    """Return the manifest defaultEmbedding outcome."""
    return Case.branch_result(EMBEDDED, EmbedOutput(id='embed-mock-1', embeddings=((0.1, 0.2, 0.3),)))

setattr(embed_default_embedding, '__module__', "dex_connectors.cohere.coheremock")

def embed_invalid_key() -> Case[EmbedOutput]:
    """Return the manifest invalidKey outcome."""
    return Case.branch_result(PROVIDER_REJECTED, EmbedOutput(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "embed", "Cohere rejected the API key."))

setattr(embed_invalid_key, '__module__', "dex_connectors.cohere.coheremock")

def rerank_ranked(value: RerankOutput, failure: Failure | None = None) -> Case[RerankOutput]:
    """Script the ranked branch."""
    return Case.branch_result(RANKED, value, failure)

setattr(rerank_ranked, '__module__', "dex_connectors.cohere.coheremock")

def rerank_provider_rejected(value: RerankOutput, failure: Failure | None = None) -> Case[RerankOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(rerank_provider_rejected, '__module__', "dex_connectors.cohere.coheremock")

def rerank_invalid_response(value: RerankOutput, failure: Failure | None = None) -> Case[RerankOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(rerank_invalid_response, '__module__', "dex_connectors.cohere.coheremock")

def rerank_defect(value: RerankOutput, failure: Failure | None = None) -> Case[RerankOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(rerank_defect, '__module__', "dex_connectors.cohere.coheremock")

def rerank_retry(failure: Failure, after_seconds: float = 0) -> Case[RerankOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(rerank_retry, '__module__', "dex_connectors.cohere.coheremock")

def rerank_default_ranking() -> Case[RerankOutput]:
    """Return the manifest defaultRanking outcome."""
    return Case.branch_result(RANKED, RerankOutput(results=(RankedDocument(index=0, relevance_score=0.92), RankedDocument(index=1, relevance_score=0.08))))

setattr(rerank_default_ranking, '__module__', "dex_connectors.cohere.coheremock")

def rerank_invalid_key() -> Case[RerankOutput]:
    """Return the manifest invalidKey outcome."""
    return Case.branch_result(PROVIDER_REJECTED, RerankOutput(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "rerank", "Cohere rejected the API key."))

setattr(rerank_invalid_key, '__module__', "dex_connectors.cohere.coheremock")


class Mock:
    """Script manifest outcomes for this connector."""

    def __init__(self, connection_name: str = "cohere") -> None:
        self.embed_script: Query[EmbedInput, EmbedOutput] = Query(EMBED_DEFINITION)
        self.embed_script.default(embed_default_embedding())
        self.rerank_script: Query[RerankInput, RerankOutput] = Query(RERANK_DEFINITION)
        self.rerank_script.default(rerank_default_ranking())
        self._connection = new_connection_with_operations(
            embed=self.embed_script,
            rerank=self.rerank_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock connection for Dex Step registration."""
        return self._connection

setattr(Mock, '__module__', "dex_connectors.cohere.coheremock")

__all__ = [
    "Mock",
    "embed_embedded",
    "embed_provider_rejected",
    "embed_invalid_response",
    "embed_defect",
    "embed_retry",
    "embed_default_embedding",
    "embed_invalid_key",
    "rerank_ranked",
    "rerank_provider_rejected",
    "rerank_invalid_response",
    "rerank_defect",
    "rerank_retry",
    "rerank_default_ranking",
    "rerank_invalid_key",
]
