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


# ListKnowledgeEntriesInput selects one knowledge page. Zero page and limit use 1 and 20.
@dataclass(frozen=True, slots=True)
class ListKnowledgeEntriesInput:
    # Page is the one-based page number; zero selects the first page.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Limit is the number of entries to request, from 1 to 100; zero uses 20.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListKnowledgeEntriesInput, '__module__', 'dex_connectors.tiankechat.models')


# KnowledgeEntry contains metadata only, excluding questions, answers, and attachments.
@dataclass(frozen=True, slots=True)
class KnowledgeEntry:
    # ID is the provider's knowledge entry identifier.
    id: int = 0
    # Category is the entry category.
    category: str = ''
    # Active reports whether the entry is enabled.
    active: bool = False
    # HitCount is the accumulated number of matches.
    hit_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'hitCount'})


setattr(KnowledgeEntry, '__module__', 'dex_connectors.tiankechat.models')


# ListKnowledgeEntriesOutput contains one knowledge page and pagination counts.
@dataclass(frozen=True, slots=True)
class ListKnowledgeEntriesOutput:
    # Entries are the returned knowledge metadata in provider order.
    entries: tuple[KnowledgeEntry, ...] = ()
    # Total is the provider's total entry count.
    total: int = 0
    # Page is the returned one-based page number.
    page: int = 0
    # Limit is the returned page size.
    limit: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'entries', _replace_nil_slices(self.entries, 1))


ListKnowledgeEntriesOutput.__annotations__['entries'] = tuple[KnowledgeEntry, ...] | None
ListKnowledgeEntriesOutput.__dataclass_fields__['entries'].type = tuple[KnowledgeEntry, ...] | None
setattr(ListKnowledgeEntriesOutput, '__module__', 'dex_connectors.tiankechat.models')
