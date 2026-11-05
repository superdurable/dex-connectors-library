# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk.timestamps import parse_go_datetime_input

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListStoryIDsInput selects one feed snapshot. It does not select a historical time window.
@dataclass(frozen=True, slots=True)
class ListStoryIDsInput:
    # Feed is top, best, new, ask, or show. Empty selects top.
    feed: str = ''
    # Limit bounds returned IDs from 1 through 500. Zero selects 50.
    limit: int = 0


setattr(ListStoryIDsInput, '__module__', 'dex_connectors.hackernews.models')


# StoryIDs contains the start of a feed in the provider's current display order.
@dataclass(frozen=True, slots=True)
class StoryIDs:
    # IDs contains at most the requested limit. Top stories can include jobs.
    ids: tuple[int, ...] = ()
    # HasMore reports that the API returned more IDs than this operation retained. The API has no page cursor.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})
    # ObservedAt is the UTC time when this response was read.
    observed_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'observedAt', 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.observed_at, str):
            object.__setattr__(self, 'observed_at', parse_go_datetime_input(self.observed_at, 'observed_at'))
        object.__setattr__(self, 'ids', _replace_nil_slices(self.ids, 1))


StoryIDs.__annotations__['ids'] = tuple[int, ...] | None
StoryIDs.__dataclass_fields__['ids'].type = tuple[int, ...] | None
setattr(StoryIDs, '__module__', 'dex_connectors.hackernews.models')


# GetItemInput identifies one story, comment, job, poll, or poll option.
@dataclass(frozen=True, slots=True)
class GetItemInput:
    # ID is a positive Hacker News item ID.
    id: int = 0


setattr(GetItemInput, '__module__', 'dex_connectors.hackernews.models')


# Item preserves public API fields and adds the exact Hacker News permalink.
# TextHTML and Title are untrusted provider text. Applications must escape or sanitize them before HTML rendering.
@dataclass(frozen=True, slots=True)
class Item:
    # ID identifies this item.
    id: int = 0
    # Type is story, comment, job, poll, or pollopt.
    type: str = ''
    # By is the author's public username, when available.
    by: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Time is the creation time in Unix seconds, when available.
    time: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Title is the provider's title, which can contain HTML entities.
    title: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # TextHTML is the submitted story or comment body. Linked article content is not included.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # URL is the submitted external URL. Ask HN and other text posts may have no URL.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # DiscussionURL links directly to this story or comment on Hacker News.
    discussion_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'discussionUrl'})
    # Score is the story score. Hacker News does not expose comment scores.
    score: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Descendants is the provider's comment count for a story or poll.
    descendants: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Parent identifies a comment's parent item or a poll option's poll.
    parent: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Kids contains child IDs in the provider's ranked display order.
    kids: tuple[int, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Deleted reports a deleted item. GetItem selects notFound instead of returning a tombstone.
    deleted: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})
    # Dead reports a moderated item. GetItem selects notFound instead of returning it.
    dead: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})


setattr(Item, '__module__', 'dex_connectors.hackernews.models')
