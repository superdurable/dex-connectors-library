# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Algolia index searches and record replacements."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class SearchInput:
    """Index name, search phrase (blank matches all), and hits per page (zero uses Algolia's default)."""

    index_name: str = field(default="", metadata={"dex_json_name": "indexName"})
    query: str = ""
    hits_per_page: int = field(
        default=0, metadata={"dex_json_name": "hitsPerPage", "dex_json_omit_default": True}
    )


@dataclass(frozen=True, slots=True)
class SearchHit:
    """A matching record's stable object ID and optional display name."""

    object_id: str = field(default="", metadata={"dex_json_name": "objectID"})
    name: str = field(default="", metadata={"dex_json_omit_default": True})


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


@dataclass(frozen=True, slots=True)
class SavedObject:
    """The replaced record's object ID and Algolia's queued indexing task ID."""

    object_id: str = field(default="", metadata={"dex_json_name": "objectID"})
    task_id: int = field(default=0, metadata={"dex_json_name": "taskID"})
