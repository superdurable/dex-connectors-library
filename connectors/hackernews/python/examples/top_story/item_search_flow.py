# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one page of keyword search results."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.hackernews import (
    Connection,
    ItemSearchPage,
    SearchItemsInput,
    new_search_items_step,
)

CONNECTION_NAME = "hackernews"


@dataclass(frozen=True)
class ItemSearchRequest:
    """Search keywords and zero-based result page."""

    query: str
    page: int = 0


def item_search_input(request: ItemSearchRequest) -> SearchItemsInput:
    """Read at most two hits from the selected page."""
    return SearchItemsInput(query=request.query, page=request.page, hits_per_page=2)


# dex:group group-id:search group-label:"Keyword search"
# dex:explanation text:"Start the keyword search."
class StartItemSearch(dex.Step[ItemSearchRequest]):
    def get_step_type(self) -> str:
        return "StartHackerNewsItemSearch"

    def execute(self, context: dex.Context, input: ItemSearchRequest) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ReadHackerNewsItemSearch"), input)


# dex:group group-id:search group-label:"Keyword search"
# dex:explanation text:"Return the indexed hits and next page number."
class ReturnItemSearch(dex.Step[QueryResult[ItemSearchPage]]):
    def get_step_type(self) -> str:
        return "ReturnHackerNewsItemSearch"

    def execute(self, context: dex.Context, input: QueryResult[ItemSearchPage]) -> dex.StepDecision:
        return dex.graceful_complete(input.value)


class ItemSearchFlow(dex.Flow[ItemSearchRequest]):
    """Search Hacker News item results using the Worker's static public connection."""

    def __init__(self, connection: Connection) -> None:
        self._start = StartItemSearch()
        self._complete = ReturnItemSearch()
        self._read = new_search_items_step(
            step_type="ReadHackerNewsItemSearch",
            step_input_type=ItemSearchRequest,
            annotations={
                "groupId": "search",
                "groupLabel": "Keyword search",
                "explanation": "Search public item results in provider order.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=item_search_input,
            searched=self._complete,
        )

    def get_flow_type(self) -> str:
        return "HackerNewsItemSearchPython"

    def get_steps(self) -> dex.StepList[ItemSearchRequest]:
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)
