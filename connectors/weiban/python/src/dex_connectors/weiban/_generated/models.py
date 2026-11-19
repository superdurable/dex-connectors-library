# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListTagsInput has no user-supplied parameters.
@dataclass(frozen=True, slots=True)
class ListTagsInput:
    pass


setattr(ListTagsInput, '__module__', 'dex_connectors.weiban.models')


# TagMetadata contains an authorized customer tag's identity and group, without customer data.
@dataclass(frozen=True, slots=True)
class TagMetadata:
    # ID is the provider tag ID.
    id: str = ''
    # Name is the human-readable tag name.
    name: str = ''
    # Group is the human-readable tag group.
    group: str = ''


setattr(TagMetadata, '__module__', 'dex_connectors.weiban.models')


# TagsResult contains the first ten tags and the provider's total tag count.
@dataclass(frozen=True, slots=True)
class TagsResult:
    # Total is the number of tags across all pages.
    total: int = 0
    # Tags contains up to ten tags from the first page.
    tags: tuple[TagMetadata, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'tags', _replace_nil_slices(self.tags, 1))


TagsResult.__annotations__['tags'] = tuple[TagMetadata, ...] | None
TagsResult.__dataclass_fields__['tags'].type = tuple[TagMetadata, ...] | None
setattr(TagsResult, '__module__', 'dex_connectors.weiban.models')
