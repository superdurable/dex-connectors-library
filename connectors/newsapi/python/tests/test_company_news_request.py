# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Example Flow request preparation without a Dex Worker dependency."""

from datetime import UTC, datetime

import dex
import pytest
from dex_connectors.sdk import StepAnnotations

from dex_connectors.newsapi import new_search_articles_step
from dex_connectors.newsapi.newsapimock import Mock
from examples.company_news.flow import CompanyNewsFlow
from examples.company_news.request import (
    CONNECTION_NAME,
    FLOW_TYPE,
    CompanyNewsRequest,
    map_to_search_articles_input,
    prepare_company_news_request,
)


def test_prepare_and_map_company_news() -> None:
    prepared = prepare_company_news_request(
        CompanyNewsRequest("  Meridian  "), datetime(2026, 10, 30, 7, tzinfo=UTC)
    )
    assert prepared == CompanyNewsRequest("Meridian", 10, 20, "en", "2026-10-20")
    operation_input = map_to_search_articles_input(prepared)
    assert operation_input.query == "Meridian"
    assert operation_input.search_in == ("title",)
    assert operation_input.from_time == "2026-10-20"
    assert operation_input.sort_by == "publishedAt"
    assert operation_input.page_size == 20
    assert FLOW_TYPE == "NewsAPICompanyNews" and CONNECTION_NAME == "newsapi"


@pytest.mark.parametrize(
    "news_request",
    [
        CompanyNewsRequest(" "),
        CompanyNewsRequest("x" * 101),
        CompanyNewsRequest("Meridian", days=-1),
        CompanyNewsRequest("Meridian", days=31),
        CompanyNewsRequest("Meridian", max_articles=-1),
        CompanyNewsRequest("Meridian", max_articles=101),
    ],
)
def test_invalid_company_news_request(news_request: CompanyNewsRequest) -> None:
    with pytest.raises(ValueError):
        prepare_company_news_request(news_request, datetime(2026, 10, 30, tzinfo=UTC))


def test_company_news_registers_operation_step_with_released_dex() -> None:
    flow = CompanyNewsFlow(Mock().connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    assert registry.flows == (flow,)
    assert flow.get_flow_type() == FLOW_TYPE
    assert tuple(attribute.name for attribute in flow.get_persistence_schema().attributes) == (
        "newsapi-company-news-request",
        "newsapi-company-news-outcome",
    )


def test_company_news_rejects_another_connection_name() -> None:
    with pytest.raises(ValueError, match="static newsapi connection"):
        CompanyNewsFlow(Mock("other").connection())


def test_search_articles_step_rejects_mismatched_connection_name() -> None:
    with pytest.raises(ValueError, match="connection name"):
        new_search_articles_step(
            step_type="SearchCompanyArticles",
            step_input_type=CompanyNewsRequest,
            annotations=StepAnnotations("newsapi", "NewsAPI", "Search articles."),
            connection=Mock().connection(),
            connection_name="other",
            map_to_operation_input=map_to_search_articles_input,
            searched=dex.StepRef("CompanyArticlesFound"),
        )


def test_search_articles_step_requires_complete_static_annotations() -> None:
    with pytest.raises(ValueError, match="groupId"):
        new_search_articles_step(
            step_type="SearchCompanyArticles",
            step_input_type=CompanyNewsRequest,
            annotations={"groupId": "newsapi", "groupLabel": "NewsAPI"},
            connection=Mock().connection(),
            connection_name="newsapi",
            map_to_operation_input=map_to_search_articles_input,
            searched=dex.StepRef("CompanyArticlesFound"),
        )
