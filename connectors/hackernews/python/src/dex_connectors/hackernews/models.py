# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Hacker News feed snapshots and items."""

from dataclasses import dataclass, field
from datetime import UTC, datetime


@dataclass(frozen=True, slots=True)
class ListStoryIDsInput:
    """Feed (top, best, new, ask, or show; blank means top) and limit (zero means 50, at most 500)."""

    feed: str = ""
    limit: int = 0


@dataclass(frozen=True, slots=True)
class StoryIDs:
    """Item IDs in provider order, whether the feed held more, and when the snapshot was read."""

    ids: tuple[int, ...] = ()
    has_more: bool = field(default=False, metadata={"dex_json_name": "hasMore"})
    observed_at: datetime = field(
        default_factory=lambda: datetime.min.replace(tzinfo=UTC),
        metadata={"dex_json_name": "observedAt", "dex_json_format": "rfc3339"},
    )


@dataclass(frozen=True, slots=True)
class GetItemInput:
    """Positive Hacker News item ID."""

    id: int = 0


@dataclass(frozen=True, slots=True)
class Item:
    """Public story, comment, job, poll, or poll option; text_html is untrusted provider HTML."""

    id: int = 0
    type: str = ""
    discussion_url: str = field(default="", metadata={"dex_json_name": "discussionUrl"})
    by: str = field(default="", metadata={"dex_json_omit_default": True})
    time: int = field(default=0, metadata={"dex_json_omit_default": True})
    title: str = field(default="", metadata={"dex_json_omit_default": True})
    text_html: str = field(default="", metadata={"dex_json_name": "text", "dex_json_omit_default": True})
    url: str = field(default="", metadata={"dex_json_omit_default": True})
    score: int = field(default=0, metadata={"dex_json_omit_default": True})
    descendants: int = field(default=0, metadata={"dex_json_omit_default": True})
    parent: int = field(default=0, metadata={"dex_json_omit_default": True})
    kids: tuple[int, ...] = field(default=(), metadata={"dex_json_omit_default": True})
    deleted: bool = field(default=False, metadata={"dex_json_omit_default": True})
    dead: bool = field(default=False, metadata={"dex_json_omit_default": True})
