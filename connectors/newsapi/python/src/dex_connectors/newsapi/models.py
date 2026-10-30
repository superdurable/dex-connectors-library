# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed NewsAPI search inputs and article pages."""

from dataclasses import dataclass, field
from datetime import UTC, datetime


@dataclass(frozen=True, slots=True)
class SearchArticlesInput:
    """One everything search; a query, source, or domain is required."""

    query: str = field(default="", metadata={"dex_json_omit_default": True})
    search_in: tuple[str, ...] = field(
        default=(), metadata={"dex_json_name": "searchIn", "dex_json_omit_default": True}
    )
    sources: tuple[str, ...] = field(default=(), metadata={"dex_json_omit_default": True})
    domains: tuple[str, ...] = field(default=(), metadata={"dex_json_omit_default": True})
    exclude_domains: tuple[str, ...] = field(
        default=(), metadata={"dex_json_name": "excludeDomains", "dex_json_omit_default": True}
    )
    from_time: str = field(default="", metadata={"dex_json_name": "from", "dex_json_omit_default": True})
    to_time: str = field(default="", metadata={"dex_json_name": "to", "dex_json_omit_default": True})
    language: str = field(default="", metadata={"dex_json_omit_default": True})
    sort_by: str = field(default="", metadata={"dex_json_name": "sortBy", "dex_json_omit_default": True})
    page_size: int = field(default=0, metadata={"dex_json_name": "pageSize", "dex_json_omit_default": True})
    page: int = field(default=0, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class ArticleSource:
    """NewsAPI publisher identity; ID can be absent."""

    id: str | None = None
    name: str = ""


@dataclass(frozen=True, slots=True)
class Article:
    """Provider article, including nullable fields and untrusted display text."""

    source: ArticleSource = field(default_factory=ArticleSource)
    author: str | None = None
    title: str = ""
    description: str | None = None
    url: str = ""
    url_to_image: str | None = field(default=None, metadata={"dex_json_name": "urlToImage"})
    published_at: datetime = field(
        default_factory=lambda: datetime.min.replace(tzinfo=UTC),
        metadata={"dex_json_name": "publishedAt", "dex_json_format": "rfc3339"},
    )
    content: str | None = None


@dataclass(frozen=True, slots=True)
class ArticlePage:
    """One page of article matches and NewsAPI's total result count."""

    total_results: int = field(default=0, metadata={"dex_json_name": "totalResults"})
    articles: tuple[Article, ...] = ()
    page: int = 0
    has_more: bool = field(default=False, metadata={"dex_json_name": "hasMore"})
