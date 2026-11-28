# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one page of keyword search results."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.hackernews import (
    Connection,
    SearchStoriesInput,
    StorySearchPage,
    new_search_stories_step,
)

CONNECTION_NAME = "hackernews"


@dataclass(frozen=True)
class StorySearchRequest:
    """Search keywords and zero-based result page."""

    query: str
    page: int = 0


def story_search_input(request: StorySearchRequest) -> SearchStoriesInput:
    """Read at most two hits from the selected page."""
    return SearchStoriesInput(query=request.query, page=request.page, hits_per_page=2)


# dex:group group-id:search group-label:"Keyword search"
# dex:explanation text:"Start the keyword search."
class StartStorySearch(dex.Step[StorySearchRequest]):
    def get_step_type(self) -> str:
        return "StartHackerNewsStorySearch"

    def execute(self, context: dex.Context, input: StorySearchRequest) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ReadHackerNewsStorySearch"), input)


# dex:group group-id:search group-label:"Keyword search"
# dex:explanation text:"Return the indexed hits and next page number."
class ReturnStorySearch(dex.Step[QueryResult[StorySearchPage]]):
    def get_step_type(self) -> str:
        return "ReturnHackerNewsStorySearch"

    def execute(self, context: dex.Context, input: QueryResult[StorySearchPage]) -> dex.StepDecision:
        return dex.graceful_complete(input.value)


class StorySearchFlow(dex.Flow[StorySearchRequest]):
    """Search Hacker News story results using the Worker's static public connection."""

    def __init__(self, connection: Connection) -> None:
        self._start = StartStorySearch()
        self._complete = ReturnStorySearch()
        self._read = new_search_stories_step(
            step_type="ReadHackerNewsStorySearch",
            step_input_type=StorySearchRequest,
            annotations={
                "groupId": "search",
                "groupLabel": "Keyword search",
                "explanation": "Search public story results in provider order.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=story_search_input,
            searched=self._complete,
        )

    def get_flow_type(self) -> str:
        return "HackerNewsStorySearchPython"

    def get_steps(self) -> dex.StepList[StorySearchRequest]:
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)
