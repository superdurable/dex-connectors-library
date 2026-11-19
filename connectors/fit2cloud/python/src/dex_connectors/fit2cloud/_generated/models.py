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


# ListPostsInput selects a one-based article page; zero values use page 1 and 20 articles.
@dataclass(frozen=True, slots=True)
class ListPostsInput:
    # Page is the one-based article page number.
    page: int = 0
    # PageSize is the number of articles requested, from 1 to 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})


setattr(ListPostsInput, '__module__', 'dex_connectors.fit2cloud.models')


# PostSummary contains article identity and publication metadata without content or author details.
@dataclass(frozen=True, slots=True)
class PostSummary:
    # Name is the stable Halo metadata name.
    name: str = ''
    # Title is the article title.
    title: str = ''
    # Slug is the article URL slug.
    slug: str = ''
    # Published reports whether the article is published.
    published: bool = False
    # Visibility is PUBLIC, INTERNAL, or PRIVATE.
    visibility: str = ''


setattr(PostSummary, '__module__', 'dex_connectors.fit2cloud.models')


# ListPostsOutput contains one article page and the next page number, or zero at the end.
@dataclass(frozen=True, slots=True)
class ListPostsOutput:
    # Posts are the returned articles in provider order.
    posts: tuple[PostSummary, ...] = ()
    # NextPage is the next one-based page number, or zero if the page is terminal.
    next_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextPage'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'posts', _replace_nil_slices(self.posts, 1))


ListPostsOutput.__annotations__['posts'] = tuple[PostSummary, ...] | None
ListPostsOutput.__dataclass_fields__['posts'].type = tuple[PostSummary, ...] | None
setattr(ListPostsOutput, '__module__', 'dex_connectors.fit2cloud.models')
