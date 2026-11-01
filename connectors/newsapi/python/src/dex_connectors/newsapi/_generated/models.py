# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return value
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# SearchArticlesInput selects one page of NewsAPI's everything search.
# Query, Sources, or Domains must be set. Empty fields are not sent, so NewsAPI applies its own defaults.
@dataclass(frozen=True, slots=True)
class SearchArticlesInput:
    # Query is NewsAPI's keyword expression, up to 500 characters. It supports quoted phrases,
    # + and - prefixes, AND, OR, NOT, and parentheses. The connector form-encodes it.
    query: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # SearchIn limits Query matching to title, description, or content. Empty searches all three.
    search_in: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'searchIn', 'dex_json_omit_default': True})
    # Sources lists up to 20 NewsAPI source IDs, such as bbc-news.
    sources: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Domains restricts results to these domains, such as techcrunch.com.
    domains: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ExcludeDomains removes results from these domains.
    exclude_domains: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'excludeDomains', 'dex_json_omit_default': True})
    # From is the oldest publication time, as a date such as 2026-01-05, a UTC time such as
    # 2026-01-05T07:00:00, or an RFC 3339 time. Empty uses the oldest article the plan allows.
    from_: str = _dataclass_field(default='', metadata={'dex_json_name': 'from', 'dex_json_omit_default': True})
    # To is the newest publication time in the same forms. Empty means now.
    to: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Language is one of Languages. Empty returns articles in every language.
    language: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # SortBy is publishedAt (newest first), relevancy, or popularity. Empty uses NewsAPI's default, publishedAt.
    sort_by: str = _dataclass_field(default='', metadata={'dex_json_name': 'sortBy', 'dex_json_omit_default': True})
    # PageSize is the number of articles per page, from 1 through 100. Zero uses NewsAPI's default of 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Page is the 1-based page number. Zero selects page 1.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(SearchArticlesInput, '__module__', 'dex_connectors.newsapi.models')


# ArticleSource names an article's publisher.
@dataclass(frozen=True, slots=True)
class ArticleSource:
    # ID is NewsAPI's source ID, such as bbc-news, or nil for a publisher without one.
    id: str | None = None
    # Name is the publisher's display name.
    name: str = ''


setattr(ArticleSource, '__module__', 'dex_connectors.newsapi.models')


# Article preserves NewsAPI's article fields. A pointer field is nil when NewsAPI sends null, so an
# application can tell a missing value from an empty one. NewsAPI keeps a removed article in results
# with the title [Removed]; the connector returns it unchanged.
# Title, Description, and Content are untrusted provider text: escape them before rendering HTML.
@dataclass(frozen=True, slots=True)
class Article:
    # Source identifies the publisher.
    source: ArticleSource = _dataclass_field(default_factory=ArticleSource)
    # Author is the byline, or nil when NewsAPI has none.
    author: str | None = None
    # Title is the headline.
    title: str = ''
    # Description is the article summary, or nil when NewsAPI has none.
    description: str | None = None
    # URL links to the article on the publisher's site.
    url: str = ''
    # URLToImage links to the article's lead image, or is nil.
    url_to_image: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'urlToImage'})
    # PublishedAt is the publication time in UTC.
    published_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'publishedAt', 'dex_json_format': 'rfc3339'})
    # Content is NewsAPI's excerpt of the body, truncated by NewsAPI to about 200 characters, or nil.
    content: str | None = None


setattr(Article, '__module__', 'dex_connectors.newsapi.models')


# ArticlePage is one page of matching articles in NewsAPI's order.
@dataclass(frozen=True, slots=True)
class ArticlePage:
    # TotalResults is NewsAPI's count of all matching articles. The Developer plan returns only the first 100.
    total_results: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalResults'})
    # Articles holds this page, possibly empty.
    articles: tuple[Article, ...] = ()
    # Page is the 1-based page this result holds.
    page: int = 0
    # HasMore reports that TotalResults exceeds the articles up to and including this page.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'articles', _replace_nil_slices(self.articles, 1))


ArticlePage.__annotations__['articles'] = tuple[Article, ...] | None
setattr(ArticlePage, '__module__', 'dex_connectors.newsapi.models')
