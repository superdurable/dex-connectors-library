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


# ListTagsInput selects a one-based page and page size. Zero values use page 1 and 20 tags.
@dataclass(frozen=True, slots=True)
class ListTagsInput:
    # Page is a one-based page number.
    page: int = 0
    # PageSize is the requested tags per page, from 1 to 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})


setattr(ListTagsInput, '__module__', 'dex_connectors.jingdigital.models')


# TagSummary exposes only ordinary tag metadata.
@dataclass(frozen=True, slots=True)
class TagSummary:
    # ID is the provider's numeric tag ID.
    id: int = 0
    # Name is the marketing tag name.
    name: str = ''
    # Level is the provider's tag hierarchy level.
    level: int = 0


setattr(TagSummary, '__module__', 'dex_connectors.jingdigital.models')


# ListTagsOutput contains a bounded page and pagination metadata.
@dataclass(frozen=True, slots=True)
class ListTagsOutput:
    # Tags are returned in provider order.
    tags: tuple[TagSummary, ...] = ()
    # Total is the number of matching tags reported by JINGdigital.
    total: int = 0
    # NextPage is the next one-based page or zero when terminal.
    next_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextPage'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'tags', _replace_nil_slices(self.tags, 1))


ListTagsOutput.__annotations__['tags'] = tuple[TagSummary, ...] | None
ListTagsOutput.__dataclass_fields__['tags'].type = tuple[TagSummary, ...] | None
setattr(ListTagsOutput, '__module__', 'dex_connectors.jingdigital.models')
