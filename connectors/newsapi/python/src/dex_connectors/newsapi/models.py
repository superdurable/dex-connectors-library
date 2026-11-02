# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed NewsAPI search inputs and article pages."""

from dataclasses import dataclass, field

from ._generated.models import Article as Article
from ._generated.models import ArticlePage as ArticlePage
from ._generated.models import ArticleSource as ArticleSource


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


ArticleSource.__doc__ = "NewsAPI publisher identity; ID can be absent."
Article.__doc__ = "Provider article, including nullable fields and untrusted display text."
ArticlePage.__doc__ = "One page of article matches and NewsAPI's total result count."
