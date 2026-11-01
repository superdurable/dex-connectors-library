# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass, field

from ._generated.models import (
    SavedObject as SavedObject,
)
from ._generated.models import (
    SearchHit as SearchHit,
)
from ._generated.models import (
    SearchInput as SearchInput,
)


@dataclass(frozen=True, slots=True)
class SearchPage:
    """Algolia's count of matching records and the returned record identities."""

    nb_hits: int = field(default=0, metadata={"dex_json_name": "nbHits"})
    hits: tuple[SearchHit, ...] = ()


@dataclass(frozen=True, slots=True)
class SaveObjectInput:
    """Target index, stable object ID, and the record's complete non-secret string attributes."""

    index_name: str = field(default="", metadata={"dex_json_name": "indexName"})
    object_id: str = field(default="", metadata={"dex_json_name": "objectID"})
    attributes: dict[str, str] = field(default_factory=dict)
