# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed NewsAPI search scripts for consuming Flow tests."""

from datetime import UTC, datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..client import (
    CONNECTOR_ID,
    DEFECT,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    SEARCH_ARTICLES_DEFINITION,
    SEARCHED,
)
from ..connection import Connection, new_connection_with_operations
from ..models import Article, ArticlePage, ArticleSource, SearchArticlesInput


def search_articles_searched(value: ArticlePage, failure: Failure | None = None) -> Case[ArticlePage]:
    """Script the successful searched route."""
    return Case.branch_result(SEARCHED, value, failure)


def search_articles_provider_rejected(
    value: ArticlePage, failure: Failure | None = None
) -> Case[ArticlePage]:
    """Script a definite provider rejection."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)


def search_articles_invalid_response(value: ArticlePage, failure: Failure | None = None) -> Case[ArticlePage]:
    """Script malformed or oversized provider data."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)


def search_articles_defect(value: ArticlePage, failure: Failure | None = None) -> Case[ArticlePage]:
    """Script a local validation or connection defect."""
    return Case.branch_result(DEFECT, value, failure)


def search_articles_retry(failure: Failure, after_seconds: float = 0) -> Case[ArticlePage]:
    """Request another Dex Execute attempt."""
    return Case.retry(failure, after_seconds)


def search_articles_searched_default_answer() -> Case[ArticlePage]:
    """Return the manifest's two Meridian article search result."""
    publisher = ArticleSource("meridian-news", "Meridian News")
    return search_articles_searched(
        ArticlePage(
            2,
            (
                Article(
                    publisher,
                    "A. Rivera",
                    "Meridian opens a research lab",
                    "The company opens a new lab.",
                    "https://news.example.test/meridian-lab",
                    None,
                    datetime(2026, 10, 26, 12, tzinfo=UTC),
                    "Meridian opened its newest research lab.",
                ),
                Article(
                    publisher,
                    None,
                    "Meridian reports quarterly results",
                    None,
                    "https://news.example.test/meridian-results",
                    None,
                    datetime(2026, 10, 25, 12, tzinfo=UTC),
                    None,
                ),
            ),
            1,
            False,
        )
    )


def search_articles_invalid_key() -> Case[ArticlePage]:
    """Return the manifest's invalid API key scenario."""
    return search_articles_provider_rejected(
        ArticlePage(),
        Failure(
            FailureKind.AUTHENTICATION,
            CONNECTOR_ID,
            "searchArticles",
            "Searching with an invalid NewsAPI key is rejected.",
        ),
    )


class Mock:
    """Script one NewsAPI Query, defaulting to its manifest answer."""

    def __init__(self, connection_name: str = "newsapi") -> None:
        self.search_articles_script: Query[SearchArticlesInput, ArticlePage] = Query(
            SEARCH_ARTICLES_DEFINITION
        )
        self.search_articles_script.default(search_articles_searched_default_answer())
        self._connection = new_connection_with_operations(
            self.search_articles_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection

    def search_articles(self) -> Query[SearchArticlesInput, ArticlePage]:
        """Return the SDK mock operation for Flow Step registration."""
        return self.search_articles_script


__all__ = [
    "Mock",
    "search_articles_searched",
    "search_articles_provider_rejected",
    "search_articles_invalid_response",
    "search_articles_defect",
    "search_articles_retry",
    "search_articles_searched_default_answer",
    "search_articles_invalid_key",
]
