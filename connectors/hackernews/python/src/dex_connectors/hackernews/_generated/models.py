# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime

@dataclass(frozen=True, slots=True)
class ListStoryIDsInput:
    """Typed ListStoryIDsInput value from the Go connector contract."""

    feed: str = ''
    limit: int = 0


setattr(ListStoryIDsInput, '__module__', 'dex_connectors.hackernews.models')


@dataclass(frozen=True, slots=True)
class StoryIDs:
    """Typed StoryIDs value from the Go connector contract."""

    ids: tuple[int, ...] | None = None
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})
    observed_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'observedAt', 'dex_json_format': 'rfc3339'})


setattr(StoryIDs, '__module__', 'dex_connectors.hackernews.models')


@dataclass(frozen=True, slots=True)
class GetItemInput:
    """Typed GetItemInput value from the Go connector contract."""

    id: int = 0


setattr(GetItemInput, '__module__', 'dex_connectors.hackernews.models')


@dataclass(frozen=True, slots=True)
class Item:
    """Typed Item value from the Go connector contract."""

    id: int = 0
    type: str = ''
    by: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    time: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    title: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    discussion_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'discussionUrl'})
    score: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    descendants: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    parent: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    kids: tuple[int, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    deleted: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})
    dead: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})


setattr(Item, '__module__', 'dex_connectors.hackernews.models')
