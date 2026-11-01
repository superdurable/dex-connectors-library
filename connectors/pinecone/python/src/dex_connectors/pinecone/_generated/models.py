# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# Vector is one dense Pinecone vector, replaced by ID during upsert.
@dataclass(frozen=True, slots=True)
class Vector:
    # ID is the stable vector identity.
    id: str = ''
    # Values contains the dense vector coordinates.
    values: tuple[float, ...] = ()
    # Metadata contains optional string-valued filterable attributes.
    metadata: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'values', _replace_nil_slices(self.values, 1))


Vector.__annotations__['values'] = tuple[float, ...] | None
Vector.__dataclass_fields__['values'].type = tuple[float, ...] | None
setattr(Vector, '__module__', 'dex_connectors.pinecone.models')


# UpsertVectorsInput writes one bounded vector batch.
@dataclass(frozen=True, slots=True)
class UpsertVectorsInput:
    # Namespace selects the index namespace; blank uses Pinecone's default.
    namespace: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Vectors are replaced by ID; each ID must be stable across retries.
    vectors: tuple[Vector, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'vectors', _replace_nil_slices(self.vectors, 1))


UpsertVectorsInput.__annotations__['vectors'] = tuple[Vector, ...] | None
UpsertVectorsInput.__dataclass_fields__['vectors'].type = tuple[Vector, ...] | None
setattr(UpsertVectorsInput, '__module__', 'dex_connectors.pinecone.models')


# UpsertVectorsOutput reports Pinecone's accepted vector count.
@dataclass(frozen=True, slots=True)
class UpsertVectorsOutput:
    # UpsertedCount is the number of vectors accepted.
    upserted_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'upsertedCount'})


setattr(UpsertVectorsOutput, '__module__', 'dex_connectors.pinecone.models')


# QueryVectorsInput searches by one dense vector.
@dataclass(frozen=True, slots=True)
class QueryVectorsInput:
    # Namespace selects the index namespace.
    namespace: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Vector is the dense query vector.
    vector: tuple[float, ...] = ()
    # TopK is the result limit from 1 through 10000.
    top_k: int = _dataclass_field(default=0, metadata={'dex_json_name': 'topK'})
    # IncludeMetadata requests stored metadata in matches.
    include_metadata: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'includeMetadata', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'vector', _replace_nil_slices(self.vector, 1))


QueryVectorsInput.__annotations__['vector'] = tuple[float, ...] | None
QueryVectorsInput.__dataclass_fields__['vector'].type = tuple[float, ...] | None
setattr(QueryVectorsInput, '__module__', 'dex_connectors.pinecone.models')


# QueryVectorsOutput preserves Pinecone's JSON response.
@dataclass(frozen=True, slots=True)
class QueryVectorsOutput:
    # Data contains matches and the namespace, with provider fields preserved.
    data: Any = None


setattr(QueryVectorsOutput, '__module__', 'dex_connectors.pinecone.models')
