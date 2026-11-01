# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class SearchInput:
    """Typed SearchInput value from the Go connector contract."""

    index_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'indexName'})
    query: str = ''
    hits_per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'hitsPerPage', 'dex_json_omit_default': True})


setattr(SearchInput, '__module__', 'dex_connectors.algolia.models')


@dataclass(frozen=True, slots=True)
class SearchHit:
    """Typed SearchHit value from the Go connector contract."""

    object_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'objectID'})
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(SearchHit, '__module__', 'dex_connectors.algolia.models')


@dataclass(frozen=True, slots=True)
class SearchPage:
    """Typed SearchPage value from the Go connector contract."""

    nb_hits: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nbHits'})
    hits: tuple[SearchHit, ...] | None = None


setattr(SearchPage, '__module__', 'dex_connectors.algolia.models')


@dataclass(frozen=True, slots=True)
class SaveObjectInput:
    """Typed SaveObjectInput value from the Go connector contract."""

    index_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'indexName'})
    object_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'objectID'})
    attributes: dict[str, str] | None = None


setattr(SaveObjectInput, '__module__', 'dex_connectors.algolia.models')


@dataclass(frozen=True, slots=True)
class SavedObject:
    """Typed SavedObject value from the Go connector contract."""

    object_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'objectID'})
    task_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'taskID'})


setattr(SavedObject, '__module__', 'dex_connectors.algolia.models')
