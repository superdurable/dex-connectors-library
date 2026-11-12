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


# SaveObjectInput replaces a record's complete set of string attributes.
@dataclass(frozen=True, slots=True)
class SaveObjectInput:
    # IndexName names the target index.
    index_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'indexName'})
    # ObjectID identifies the record to replace on retries.
    object_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'objectID'})
    # Attributes are absolute non-secret record values; omitted old attributes are removed.
    attributes: dict[str, str] | None = None


setattr(SaveObjectInput, '__module__', 'dex_connectors.algolia.models')


# SavedObject confirms Algolia queued an indexing task.
@dataclass(frozen=True, slots=True)
class SavedObject:
    # ObjectID identifies the replaced record.
    object_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'objectID'})
    # TaskID identifies the queued Algolia indexing task.
    task_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'taskID'})


setattr(SavedObject, '__module__', 'dex_connectors.algolia.models')


# SearchInput selects an index and search phrase.
@dataclass(frozen=True, slots=True)
class SearchInput:
    # IndexName names the Algolia index.
    index_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'indexName'})
    # Query is the search phrase; blank matches all records.
    query: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # HitsPerPage bounds the returned hits; zero uses Algolia's default.
    hits_per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'hitsPerPage', 'dex_json_omit_default': True})


setattr(SearchInput, '__module__', 'dex_connectors.algolia.models')


# SearchHit retains only identity and display name from a matching record.
@dataclass(frozen=True, slots=True)
class SearchHit:
    # ObjectID is the record's stable ID.
    object_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'objectID'})
    # Name is an optional record name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(SearchHit, '__module__', 'dex_connectors.algolia.models')


# SearchPage contains a bounded Algolia hit page.
@dataclass(frozen=True, slots=True)
class SearchPage:
    # NbHits is Algolia's count of matching records.
    nb_hits: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nbHits'})
    # Hits contains the returned record identities.
    hits: tuple[SearchHit, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'hits', _replace_nil_slices(self.hits, 1))


SearchPage.__annotations__['hits'] = tuple[SearchHit, ...] | None
SearchPage.__dataclass_fields__['hits'].type = tuple[SearchHit, ...] | None
setattr(SearchPage, '__module__', 'dex_connectors.algolia.models')
