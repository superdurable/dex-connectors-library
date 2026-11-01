# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime

@dataclass(frozen=True, slots=True)
class SearchArticlesInput:
    """Typed SearchArticlesInput value from the Go connector contract."""

    query: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    search_in: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'searchIn', 'dex_json_omit_default': True})
    sources: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    domains: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    exclude_domains: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'excludeDomains', 'dex_json_omit_default': True})
    from_: str = _dataclass_field(default='', metadata={'dex_json_name': 'from', 'dex_json_omit_default': True})
    to: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    language: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    sort_by: str = _dataclass_field(default='', metadata={'dex_json_name': 'sortBy', 'dex_json_omit_default': True})
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(SearchArticlesInput, '__module__', 'dex_connectors.newsapi.models')


@dataclass(frozen=True, slots=True)
class ArticleSource:
    """Typed ArticleSource value from the Go connector contract."""

    id: str | None = None
    name: str = ''


setattr(ArticleSource, '__module__', 'dex_connectors.newsapi.models')


@dataclass(frozen=True, slots=True)
class Article:
    """Typed Article value from the Go connector contract."""

    source: ArticleSource = _dataclass_field(default_factory=ArticleSource)
    author: str | None = None
    title: str = ''
    description: str | None = None
    url: str = ''
    url_to_image: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'urlToImage'})
    published_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'publishedAt', 'dex_json_format': 'rfc3339'})
    content: str | None = None


setattr(Article, '__module__', 'dex_connectors.newsapi.models')


@dataclass(frozen=True, slots=True)
class ArticlePage:
    """Typed ArticlePage value from the Go connector contract."""

    total_results: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalResults'})
    articles: tuple[Article, ...] | None = None
    page: int = 0
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})


setattr(ArticlePage, '__module__', 'dex_connectors.newsapi.models')
