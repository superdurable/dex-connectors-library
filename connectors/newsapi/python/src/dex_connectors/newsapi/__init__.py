# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""NewsAPI Query operation for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    DEFECT,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    SEARCH_ARTICLES_DEFINITION,
    SEARCHED,
    Client,
    Config,
    Credentials,
    SearchArticlesOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import Article, ArticlePage, ArticleSource, SearchArticlesInput
from .steps import new_search_articles_step

__all__ = [
    "Article",
    "ArticlePage",
    "ArticleSource",
    "Client",
    "Connection",
    "Config",
    "Credentials",
    "SearchArticlesInput",
    "SearchArticlesOperation",
    "new_search_articles_step",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "SEARCH_ARTICLES_DEFINITION",
    "SEARCHED",
    "PROVIDER_REJECTED",
    "INVALID_RESPONSE",
    "DEFECT",
    "CONNECTOR_ID",
]
