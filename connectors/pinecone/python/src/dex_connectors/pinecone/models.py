# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass, field

from ._generated.models import (
    QueryVectorsOutput as QueryVectorsOutput,
)
from ._generated.models import (
    UpsertVectorsOutput as UpsertVectorsOutput,
)


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
class QueryVectorsInput:
    """Namespace, dense query vector, result limit from 1 through 10000, and whether to include metadata."""

    namespace: str = field(default="", metadata={"dex_json_omit_default": True})
    vector: tuple[float, ...] = ()
    top_k: int = field(default=0, metadata={"dex_json_name": "topK"})
    include_metadata: bool = field(
        default=False, metadata={"dex_json_name": "includeMetadata", "dex_json_omit_default": True}
    )
