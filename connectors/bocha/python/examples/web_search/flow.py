# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Search public pages in a durable Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.bocha import (
    Connection,
    SearchWebInput,
    SearchWebOutput,
    new_search_web_step,
)

CONNECTION_NAME = "bocha"
_OUTCOME = dex.Attribute("bocha-web-search-outcome", SearchWebOutput)


def map_to_operation_input(input: SearchWebInput) -> SearchWebInput:
    """Pass the search phrase to the provider Query."""
    return input


# dex:group group-id:search group-label:"Web search"
# dex:explanation text:"Start searching public pages for the requested phrase."
class _StartSearch(dex.Step[SearchWebInput]):
    def get_step_type(self) -> str:
        return "StartWebSearch"

    def execute(self, context: dex.Context, input: SearchWebInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("SearchWebQuery"), input)


# dex:group group-id:search group-label:"Web search"
# dex:explanation text:"Persist the web search and complete the Flow."
class _CompleteSearch(dex.Step[QueryResult[SearchWebOutput]]):
    def get_step_type(self) -> str:
        return "CompleteWebSearch"

    def execute(self, context: dex.Context, input: QueryResult[SearchWebOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class WebSearchFlow(dex.Flow[SearchWebInput]):
    """Durably search public pages using the static Bocha connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("web search requires the static Bocha connection")
        self._start = _StartSearch()
        self._complete = _CompleteSearch()
        self._query = new_search_web_step(
            step_type="SearchWebQuery",
            step_input_type=SearchWebInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Bocha",
                "explanation": "Call Bocha searchWeb with the configured API Key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "BochaWebSearch"

    def get_steps(self) -> dex.StepList[SearchWebInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable search result Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
