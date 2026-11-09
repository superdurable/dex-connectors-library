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


# CreatePostInput creates a draft by default or publishes immediately when Status is publish.
@dataclass(frozen=True, slots=True)
class CreatePostInput:
    # Title is the nonempty post title, entered as plain text or permitted HTML.
    title: str = ''
    # Content is the post body in the site's accepted HTML or block format.
    content: str = ''
    # Status is draft or publish; empty defaults to draft.
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(CreatePostInput, '__module__', 'dex_connectors.wordpress.models')


# CreatedPost identifies a post created by WordPress without persisting its content.
@dataclass(frozen=True, slots=True)
class CreatedPost:
    # ID is the stable numeric WordPress post ID.
    id: int = 0
    # Status is the status WordPress assigned to the new post.
    status: str = ''


setattr(CreatedPost, '__module__', 'dex_connectors.wordpress.models')


# GetPostInput selects one numeric WordPress post ID.
@dataclass(frozen=True, slots=True)
class GetPostInput:
    # PostID is a positive ID from WordPress Posts or the REST API.
    post_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'postId'})


setattr(GetPostInput, '__module__', 'dex_connectors.wordpress.models')


# RenderedText holds provider HTML. Escape or sanitize it before embedding in another page.
@dataclass(frozen=True, slots=True)
class RenderedText:
    # Rendered is WordPress's rendered HTML, not plain text.
    rendered: str = ''


setattr(RenderedText, '__module__', 'dex_connectors.wordpress.models')


# Post is a published WordPress post. Title and Content contain untrusted provider HTML.
@dataclass(frozen=True, slots=True)
class Post:
    # ID is the stable numeric WordPress post ID.
    id: int = 0
    # Slug is the current URL slug and can change after an edit.
    slug: str = ''
    # Status is publish for a post returned by these operations.
    status: str = ''
    # Link is the provider's public post URL.
    link: str = ''
    # Title is the rendered title HTML.
    title: RenderedText = _dataclass_field(default_factory=RenderedText)
    # Content is the rendered body HTML.
    content: RenderedText = _dataclass_field(default_factory=RenderedText)
    # DateGMT is WordPress's UTC publication time without a zone suffix.
    date_gmt: str = _dataclass_field(default='', metadata={'dex_json_name': 'dateGMT'})


setattr(Post, '__module__', 'dex_connectors.wordpress.models')


# ListPostsInput selects one published-post page.
@dataclass(frozen=True, slots=True)
class ListPostsInput:
    # Page is one-based; zero selects page one.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # PerPage is 1 through 100; zero selects WordPress's default of ten.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage', 'dex_json_omit_default': True})
    # After is an RFC 3339 publication instant; empty has no lower bound.
    after: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListPostsInput, '__module__', 'dex_connectors.wordpress.models')


# PostPage is one published-post page and WordPress's total-count headers.
@dataclass(frozen=True, slots=True)
class PostPage:
    # Posts contains this page in WordPress's order, possibly empty.
    posts: tuple[Post, ...] = ()
    # Page is the one-based page requested.
    page: int = 0
    # Total is the matching post count reported by WordPress.
    total: int = 0
    # TotalPages is the page count reported by WordPress.
    total_pages: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalPages'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'posts', _replace_nil_slices(self.posts, 1))


PostPage.__annotations__['posts'] = tuple[Post, ...] | None
PostPage.__dataclass_fields__['posts'].type = tuple[Post, ...] | None
setattr(PostPage, '__module__', 'dex_connectors.wordpress.models')
