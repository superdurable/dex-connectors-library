# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read a public Hacker News article and its comments."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.hackernews import (
    ArticleThread,
    Connection,
    GetArticleThreadInput,
    new_get_article_thread_step,
)

CONNECTION_NAME = "hackernews"


@dataclass(frozen=True)
class ArticleRequest:
    """Positive ID from the public article permalink."""

    article_id: int


def article_input(request: ArticleRequest) -> GetArticleThreadInput:
    """Use the selected article ID for the Search API query."""
    return GetArticleThreadInput(id=request.article_id)


# dex:group group-id:article group-label:"Article thread"
# dex:explanation text:"Start the indexed article lookup."
class StartArticleThread(dex.Step[ArticleRequest]):
    def get_step_type(self) -> str:
        return "StartHackerNewsArticleThread"

    def execute(self, context: dex.Context, input: ArticleRequest) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ReadHackerNewsArticleThread"), input)


# dex:group group-id:article group-label:"Article thread"
# dex:explanation text:"Return the article and its nested comments."
class ReturnArticleThread(dex.Step[QueryResult[ArticleThread]]):
    def get_step_type(self) -> str:
        return "ReturnHackerNewsArticleThread"

    def execute(self, context: dex.Context, input: QueryResult[ArticleThread]) -> dex.StepDecision:
        return dex.graceful_complete(input.value)


class ArticleThreadFlow(dex.Flow[ArticleRequest]):
    """Read a Hacker News article thread using the Worker's static public connection."""

    def __init__(self, connection: Connection) -> None:
        self._start = StartArticleThread()
        self._complete = ReturnArticleThread()
        self._read = new_get_article_thread_step(
            step_type="ReadHackerNewsArticleThread",
            step_input_type=ArticleRequest,
            annotations={
                "groupId": "article",
                "groupLabel": "Article thread",
                "explanation": "Read the article and its indexed public comments.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=article_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        return "HackerNewsArticleThreadPython"

    def get_steps(self) -> dex.StepList[ArticleRequest]:
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)
