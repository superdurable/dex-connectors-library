# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Pinecone vector upserts and nearest-vector queries."""

from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True, slots=True)
class Vector:
    """One dense vector replaced by its stable ID, with optional string metadata."""

    id: str = ""
    values: tuple[float, ...] = ()
    metadata: dict[str, str] = field(default_factory=dict, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class UpsertVectorsInput:
    """Namespace (blank uses Pinecone's default) and 1 through 1000 vectors with unique stable IDs."""

    namespace: str = field(default="", metadata={"dex_json_omit_default": True})
    vectors: tuple[Vector, ...] = ()


@dataclass(frozen=True, slots=True)
class UpsertVectorsOutput:
    """The number of vectors Pinecone accepted."""

    upserted_count: int = field(default=0, metadata={"dex_json_name": "upsertedCount"})


@dataclass(frozen=True, slots=True)
class QueryVectorsInput:
    """Namespace, dense query vector, result limit from 1 through 10000, and whether to include metadata."""

    namespace: str = field(default="", metadata={"dex_json_omit_default": True})
    vector: tuple[float, ...] = ()
    top_k: int = field(default=0, metadata={"dex_json_name": "topK"})
    include_metadata: bool = field(
        default=False, metadata={"dex_json_name": "includeMetadata", "dex_json_omit_default": True}
    )


@dataclass(frozen=True, slots=True)
class QueryVectorsOutput:
    """Pinecone's query JSON with matches and namespace, kept untouched."""

    data: Any = None
