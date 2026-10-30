# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic input preparation for the company news example Flow."""

from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta

from dex_connectors.newsapi import Article, SearchArticlesInput

FLOW_TYPE = "NewsAPICompanyNews"
CONNECTION_NAME = "newsapi"


@dataclass(frozen=True, slots=True)
class CompanyNewsRequest:
    """Company and search window chosen at Flow start; from_time is fixed once."""

    company: str
    days: int = 0
    max_articles: int = field(default=0, metadata={"dex_json_name": "maxArticles"})
    language: str = ""
    from_time: str = field(default="", metadata={"dex_json_name": "from", "dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class CompanyNewsOutcome:
    """Found articles returned from the example Flow."""

    status: str
    company: str
    from_time: str = field(metadata={"dex_json_name": "from"})
    total_results: int = field(metadata={"dex_json_name": "totalResults"})
    articles: tuple[Article, ...]


def prepare_company_news_request(request: CompanyNewsRequest, now: datetime) -> CompanyNewsRequest:
    """Validate input and fix the search window once before provider retries."""
    company = request.company.strip()
    if not 1 <= len(company) <= 100:
        raise ValueError("company must be 1 to 100 characters")
    days = request.days or 10
    if not 1 <= days <= 30:
        raise ValueError("days must be from 1 through 30")
    max_articles = request.max_articles or 20
    if not 1 <= max_articles <= 100:
        raise ValueError("maxArticles must be from 1 through 100")
    language = request.language or "en"
    from_time = (now.astimezone(UTC) - timedelta(days=days)).strftime("%Y-%m-%d")
    return CompanyNewsRequest(company, days, max_articles, language, from_time)


def map_to_search_articles_input(request: CompanyNewsRequest) -> SearchArticlesInput:
    """Search newest title matches in the prepared publication window."""
    return SearchArticlesInput(
        query=request.company,
        search_in=("title",),
        from_time=request.from_time,
        language=request.language,
        sort_by="publishedAt",
        page_size=request.max_articles,
    )
