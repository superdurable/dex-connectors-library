# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return value
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# EmbedInput asks Cohere to embed up to 96 texts as float vectors.
@dataclass(frozen=True, slots=True)
class EmbedInput:
    # Model is a Cohere Embed model ID.
    model: str = ''
    # Texts are text inputs; the response has one vector per text.
    texts: tuple[str, ...] = ()
    # InputType is search_document, search_query, classification, or clustering.
    input_type: str = ''

    def __post_init__(self) -> None:
        object.__setattr__(self, 'texts', _replace_nil_slices(self.texts, 1))


EmbedInput.__annotations__['texts'] = tuple[str, ...] | None
setattr(EmbedInput, '__module__', 'dex_connectors.cohere.models')


# EmbedOutput contains float embeddings in input order.
@dataclass(frozen=True, slots=True)
class EmbedOutput:
    # ID is Cohere's embedding response ID.
    id: str = ''
    # Embeddings contains one float vector per text.
    embeddings: tuple[tuple[float, ...], ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'embeddings', _replace_nil_slices(self.embeddings, 2))


EmbedOutput.__annotations__['embeddings'] = tuple[tuple[float, ...] | None, ...] | None
setattr(EmbedOutput, '__module__', 'dex_connectors.cohere.models')


# RerankInput asks Cohere to rank documents for a text query.
@dataclass(frozen=True, slots=True)
class RerankInput:
    # Model is a Cohere Rerank model ID.
    model: str = ''
    # Query is the relevance question.
    query: str = ''
    # Documents are text passages to rank, in original order.
    documents: tuple[str, ...] = ()
    # TopN limits returned results; zero returns all.
    top_n: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'documents', _replace_nil_slices(self.documents, 1))


RerankInput.__annotations__['documents'] = tuple[str, ...] | None
setattr(RerankInput, '__module__', 'dex_connectors.cohere.models')


# RankedDocument identifies one input document and its relevance score.
@dataclass(frozen=True, slots=True)
class RankedDocument:
    # Index is the document's zero-based position in the input.
    index: int = 0
    # RelevanceScore is Cohere's score for that document.
    relevance_score: float = _dataclass_field(default=0.0, metadata={'dex_json_name': 'relevanceScore'})


setattr(RankedDocument, '__module__', 'dex_connectors.cohere.models')


# RerankOutput contains document indices sorted by relevance.
@dataclass(frozen=True, slots=True)
class RerankOutput:
    # Results are the ranked document indices and scores.
    results: tuple[RankedDocument, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'results', _replace_nil_slices(self.results, 1))


RerankOutput.__annotations__['results'] = tuple[RankedDocument, ...] | None
setattr(RerankOutput, '__module__', 'dex_connectors.cohere.models')
