# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

@dataclass(frozen=True, slots=True)
class Vector:
    """Typed Vector value from the Go connector contract."""

    id: str = ''
    values: tuple[float, ...] | None = None
    metadata: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})


setattr(Vector, '__module__', 'dex_connectors.pinecone.models')


@dataclass(frozen=True, slots=True)
class UpsertVectorsInput:
    """Typed UpsertVectorsInput value from the Go connector contract."""

    namespace: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    vectors: tuple[Vector, ...] | None = None


setattr(UpsertVectorsInput, '__module__', 'dex_connectors.pinecone.models')


@dataclass(frozen=True, slots=True)
class UpsertVectorsOutput:
    """Typed UpsertVectorsOutput value from the Go connector contract."""

    upserted_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'upsertedCount'})


setattr(UpsertVectorsOutput, '__module__', 'dex_connectors.pinecone.models')


@dataclass(frozen=True, slots=True)
class QueryVectorsInput:
    """Typed QueryVectorsInput value from the Go connector contract."""

    namespace: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    vector: tuple[float, ...] | None = None
    top_k: int = _dataclass_field(default=0, metadata={'dex_json_name': 'topK'})
    include_metadata: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'includeMetadata', 'dex_json_omit_default': True})


setattr(QueryVectorsInput, '__module__', 'dex_connectors.pinecone.models')


@dataclass(frozen=True, slots=True)
class QueryVectorsOutput:
    """Typed QueryVectorsOutput value from the Go connector contract."""

    data: Any = None


setattr(QueryVectorsOutput, '__module__', 'dex_connectors.pinecone.models')
