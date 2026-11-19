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


# ListUserVideosInput selects a page; empty cursor starts at the first page.
@dataclass(frozen=True, slots=True)
class ListUserVideosInput:
    # Cursor is the photo ID returned as nextCursor by the previous page.
    cursor: str = ''


setattr(ListUserVideosInput, '__module__', 'dex_connectors.kuaishou.models')


# UserVideo is bounded metadata for one authorized video.
@dataclass(frozen=True, slots=True)
class UserVideo:
    # PhotoID is Kuaishou's stable video identifier.
    photo_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'photoID'})
    # Caption is the video's title or caption.
    caption: str = ''
    # CreatedAtMilliseconds is the provider's creation time in Unix milliseconds.
    created_at_milliseconds: int = _dataclass_field(default=0, metadata={'dex_json_name': 'createdAtMilliseconds'})
    # LikeCount is the provider's reported like total.
    like_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'likeCount'})
    # CommentCount is the provider's reported comment total.
    comment_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'commentCount'})
    # ViewCount is the provider's reported view total.
    view_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'viewCount'})
    # Pending reports whether the video is still processing.
    pending: bool = False


setattr(UserVideo, '__module__', 'dex_connectors.kuaishou.models')


# UserVideosPage is one bounded page and a cursor for another request.
@dataclass(frozen=True, slots=True)
class UserVideosPage:
    # Videos are the records returned by the provider.
    videos: tuple[UserVideo, ...] = ()
    # NextCursor is the final photo ID, or empty when this page has no videos.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'videos', _replace_nil_slices(self.videos, 1))


UserVideosPage.__annotations__['videos'] = tuple[UserVideo, ...] | None
UserVideosPage.__dataclass_fields__['videos'].type = tuple[UserVideo, ...] | None
setattr(UserVideosPage, '__module__', 'dex_connectors.kuaishou.models')
