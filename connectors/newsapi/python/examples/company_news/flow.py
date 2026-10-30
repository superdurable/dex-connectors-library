# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable company news Flow using the NewsAPI searchArticles Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.newsapi import (
    ArticlePage,
    Connection,
    new_search_articles_step,
)

from .request import (
    CONNECTION_NAME,
    CompanyNewsOutcome,
    CompanyNewsRequest,
    map_to_search_articles_input,
    prepare_company_news_request,
)

_SEARCH_STEP_TYPE = "SearchCompanyArticles"
_REQUEST = dex.Attribute("newsapi-company-news-request", CompanyNewsRequest)
_OUTCOME = dex.Attribute("newsapi-company-news-outcome", CompanyNewsOutcome)


# dex:group group-id:company-news group-label:"Company news"
# dex:explanation text:"Validate the company and persist a fixed search window before calling NewsAPI."
class _RecordCompanyNewsRequest(dex.Step[CompanyNewsRequest]):
    def get_step_type(self) -> str:
        return "RecordCompanyNewsRequest"

    def execute(self, context: dex.Context, input: CompanyNewsRequest) -> dex.StepDecision:
        try:
            prepared = prepare_company_news_request(input, context.flow_started_at)
        except ValueError as error:
            return dex.force_fail(str(error))
        _REQUEST.set(context, prepared)
        return dex.go_to(dex.StepRef("SearchCompanyArticles"), prepared)


# dex:group group-id:company-news group-label:"Company news"
# dex:explanation text:"Record the matching articles and complete the Flow."
class _CompanyArticlesFound(dex.Step[QueryResult[ArticlePage]]):
    def get_step_type(self) -> str:
        return "CompanyArticlesFound"

    def execute(self, context: dex.Context, input: QueryResult[ArticlePage]) -> dex.StepDecision:
        request = _REQUEST.get(context)
        outcome = CompanyNewsOutcome(
            status="found",
            company=request.company,
            from_time=request.from_time,
            total_results=input.value.total_results,
            articles=input.value.articles,
        )
        _OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


class CompanyNewsFlow(dex.Flow[CompanyNewsRequest]):
    """Search recent title matches through the static newsapi connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("company news requires the static newsapi connection")
        self._record = _RecordCompanyNewsRequest()
        self._found = _CompanyArticlesFound()
        self._search = new_search_articles_step(
            step_type=_SEARCH_STEP_TYPE,
            step_input_type=CompanyNewsRequest,
            annotations={
                "groupId": "newsapi",
                "groupLabel": "NewsAPI",
                "explanation": "Search NewsAPI for the newest articles whose titles name the company.",
            },
            connection=connection,
            connection_name="newsapi",
            map_to_operation_input=map_to_search_articles_input,
            searched=self._found,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "NewsAPICompanyNews"

    def get_steps(self) -> dex.StepList[CompanyNewsRequest]:
        """Register request, provider search, and successful result Steps."""
        return dex.StepList.start_step(self._record).other_steps(self._search, self._found)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the prepared request and completed article page."""
        return dex.PersistenceSchema.of(_REQUEST, _OUTCOME)
