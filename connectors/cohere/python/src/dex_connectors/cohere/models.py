# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Cohere embedding and rerank requests and results."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class EmbedInput:
    """Model, 1 to 96 nonblank texts, and an input type from INPUT_TYPES, such as search_query."""

    model: str = ""
    texts: tuple[str, ...] = ()
    input_type: str = ""


@dataclass(frozen=True, slots=True)
class EmbedOutput:
    """Cohere response ID and one float vector per input text, in input order."""

    id: str = ""
    embeddings: tuple[tuple[float, ...], ...] = ()


@dataclass(frozen=True, slots=True)
class RerankInput:
    """Model, query, 1 to 1000 nonblank documents, and an optional top_n no larger than the document count."""

    model: str = ""
    query: str = ""
    documents: tuple[str, ...] = ()
    top_n: int = field(default=0, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class RankedDocument:
    """Input document index and its relevance score."""

    index: int = 0
    relevance_score: float = field(default=0.0, metadata={"dex_json_name": "relevanceScore"})


@dataclass(frozen=True, slots=True)
class RerankOutput:
    """Documents in Cohere's ranked order."""

    results: tuple[RankedDocument, ...] = ()
