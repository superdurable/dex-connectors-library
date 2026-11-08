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


# ListPostsInput selects a Facebook Page and an optional feed cursor.
@dataclass(frozen=True, slots=True)
class ListPostsInput:
    # PageID is the numeric Page ID from Meta Business Suite.
    page_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageId'})
    # After is Meta's opaque cursor from a prior page; blank starts at the first page.
    after: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListPostsInput, '__module__', 'dex_connectors.meta_facebook_pages.models')


# PagePostMetadata contains one post's public identity and display fields.
@dataclass(frozen=True, slots=True)
class PagePostMetadata:
    # ID is Meta's stable Page post ID.
    id: str = ''
    # Message is the optional text visible on the post.
    message: str = ''
    # CreatedTime is Meta's raw post timestamp.
    created_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'createdTime'})
    # PermalinkURL is the optional public HTTPS URL of the post.
    permalink_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'permalinkUrl'})


setattr(PagePostMetadata, '__module__', 'dex_connectors.meta_facebook_pages.models')


# PagePostsPage contains one bounded Page feed page.
@dataclass(frozen=True, slots=True)
class PagePostsPage:
    # Posts are the feed posts in provider order.
    posts: tuple[PagePostMetadata, ...] = ()
    # NextCursor is blank when Meta reported no later page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'posts', _replace_nil_slices(self.posts, 1))


PagePostsPage.__annotations__['posts'] = tuple[PagePostMetadata, ...] | None
PagePostsPage.__dataclass_fields__['posts'].type = tuple[PagePostMetadata, ...] | None
setattr(PagePostsPage, '__module__', 'dex_connectors.meta_facebook_pages.models')


# GetPostInput selects one post ID from listPosts or a Page event.
@dataclass(frozen=True, slots=True)
class GetPostInput:
    # PostID is a numeric or PageID_PostID Graph API identifier.
    post_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'postId'})


setattr(GetPostInput, '__module__', 'dex_connectors.meta_facebook_pages.models')


# PublishTextPostInput selects the Page and text to publish.
@dataclass(frozen=True, slots=True)
class PublishTextPostInput:
    # PageID is the numeric Page ID from Meta Business Suite.
    page_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageId'})
    # Message is nonempty UTF-8 text for the public Page feed.
    message: str = ''


setattr(PublishTextPostInput, '__module__', 'dex_connectors.meta_facebook_pages.models')


# PublishedPost identifies a post Meta accepted for publication.
@dataclass(frozen=True, slots=True)
class PublishedPost:
    # PostID is Meta's PageID_PostID identifier.
    post_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'postId'})


setattr(PublishedPost, '__module__', 'dex_connectors.meta_facebook_pages.models')
