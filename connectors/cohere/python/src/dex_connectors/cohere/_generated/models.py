# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class EmbedInput:
    """Typed EmbedInput value from the Go connector contract."""

    model: str = ''
    texts: tuple[str, ...] | None = None
    input_type: str = ''


setattr(EmbedInput, '__module__', 'dex_connectors.cohere.models')


@dataclass(frozen=True, slots=True)
class EmbedOutput:
    """Typed EmbedOutput value from the Go connector contract."""

    id: str = ''
    embeddings: tuple[tuple[float, ...] | None, ...] | None = None


setattr(EmbedOutput, '__module__', 'dex_connectors.cohere.models')


@dataclass(frozen=True, slots=True)
class RerankInput:
    """Typed RerankInput value from the Go connector contract."""

    model: str = ''
    query: str = ''
    documents: tuple[str, ...] | None = None
    top_n: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(RerankInput, '__module__', 'dex_connectors.cohere.models')


@dataclass(frozen=True, slots=True)
class RankedDocument:
    """Typed RankedDocument value from the Go connector contract."""

    index: int = 0
    relevance_score: float = _dataclass_field(default=0.0, metadata={'dex_json_name': 'relevanceScore'})


setattr(RankedDocument, '__module__', 'dex_connectors.cohere.models')


@dataclass(frozen=True, slots=True)
class RerankOutput:
    """Typed RerankOutput value from the Go connector contract."""

    results: tuple[RankedDocument, ...] | None = None


setattr(RerankOutput, '__module__', 'dex_connectors.cohere.models')
