# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fail when the hand-written Python contract drifts from connector.yaml."""

import tomllib
from datetime import UTC, timedelta
from pathlib import Path
from typing import Any

import yaml
from dex_connectors.sdk import FailureKind, default_connector_step_execute_retry_policy

from dex_connectors.newsapi import SEARCH_ARTICLES_DEFINITION, Article, ArticlePage
from dex_connectors.newsapi.newsapimock import (
    search_articles_invalid_key,
    search_articles_searched_default_answer,
)

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def _article_to_wire(article: Article) -> dict[str, Any]:
    return {
        "source": {"id": article.source.id, "name": article.source.name},
        "author": article.author,
        "title": article.title,
        "description": article.description,
        "url": article.url,
        "publishedAt": article.published_at.astimezone(UTC).isoformat().replace("+00:00", "Z"),
        "content": article.content,
    }


def _page_to_wire(page: ArticlePage) -> dict[str, Any]:
    return {
        "totalResults": page.total_results,
        "page": page.page,
        "hasMore": page.has_more,
        "articles": [_article_to_wire(article) for article in page.articles],
    }


def test_manifest_operation_and_mock_contract() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    package = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["metadata"]["name"] == "newsapi"
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + package["project"]["version"]
    assert package["project"]["name"] == "dex-connector-newsapi"

    operations = manifest["spec"]["operations"]
    assert len(operations) == 1
    operation = operations[0]
    assert operation["name"] == SEARCH_ARTICLES_DEFINITION.operation.operation_id
    assert SEARCH_ARTICLES_DEFINITION.operation.connector_id == manifest["metadata"]["name"]
    assert operation["kind"] == "query"
    assert operation["idempotency"] == "none"
    assert operation["authorization"] == "required"
    assert operation["inputType"] == "SearchArticlesInput"
    assert operation["outputType"] == "ArticlePage"
    assert [
        (branch["id"], branch["description"], branch.get("optional", False))
        for branch in operation["branches"]
    ] == [
        (branch.id, branch.description, branch.optional)
        for branch in SEARCH_ARTICLES_DEFINITION.branches or ()
    ]
    execution = operation["execution"]
    assert execution == {"executeMethodTimeout": "30s", "durability": "async"}
    defaults = SEARCH_ARTICLES_DEFINITION.step_defaults
    assert defaults.execute_method_timeout == timedelta(seconds=30)
    assert defaults.execute_durability == 2
    assert defaults.heartbeat_timeout == timedelta(0)
    assert defaults.execute_retry == default_connector_step_execute_retry_policy()

    mocks = operation["mocks"]
    assert {mock["name"] for mock in mocks} == {"searchedDefaultAnswer", "invalidKey"}
    by_name = {mock["name"]: mock for mock in mocks}
    searched = search_articles_searched_default_answer()
    searched_manifest = by_name["searchedDefaultAnswer"]
    assert searched_manifest["default"] is True
    assert searched_manifest["branch"] == searched.branch
    assert (
        searched_manifest["description"]
        == "Searching title matches for Meridian returns two recent articles on page one."
    )
    assert searched_manifest["output"] == _page_to_wire(searched.value or ArticlePage())
    rejected = search_articles_invalid_key()
    rejected_manifest = by_name["invalidKey"]
    assert rejected_manifest.get("default", False) is False
    assert rejected_manifest["branch"] == rejected.branch
    assert rejected_manifest["description"] == "Searching with an invalid NewsAPI key is rejected."
    assert rejected.failure is not None
    assert rejected_manifest["failure"] == {
        "kind": FailureKind.AUTHENTICATION.value,
        "message": rejected.failure.message,
    }
    assert rejected.value == ArticlePage()
