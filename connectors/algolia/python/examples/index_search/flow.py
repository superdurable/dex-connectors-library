# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Algolia Flow that searches one index."""

from dataclasses import dataclass, field, replace

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.algolia import Connection, SearchInput, SearchPage, new_search_step

FLOW_TYPE = "AlgoliaIndexSearch"
CONNECTION_NAME = "algolia"
_SEARCH_STEP_TYPE = "SearchAlgoliaIndex"
_HITS_PER_PAGE = 10
_OUTCOME = dex.Attribute("algolia-index-search-outcome", SearchPage)


@dataclass(frozen=True, slots=True)
class IndexSearchRequest:
    """An existing index in the connected application and a search phrase; blank matches all records."""

    index_name: str = field(metadata={"dex_json_name": "indexName"})
    query: str = ""


def map_to_search_input(request: IndexSearchRequest) -> SearchInput:
    """Search the validated index for one bounded page."""
    return SearchInput(index_name=request.index_name, query=request.query, hits_per_page=_HITS_PER_PAGE)


# dex:group group-id:index-search group-label:"Index search"
# dex:explanation text:"Validate the index name before searching Algolia."
class _Start(dex.Step[IndexSearchRequest]):
    def get_step_type(self) -> str:
        return "StartAlgoliaIndexSearch"

    def execute(self, context: dex.Context, input: IndexSearchRequest) -> dex.StepDecision:
        request = replace(input, index_name=input.index_name.strip())
        if not request.index_name:
            return dex.force_fail("indexName is required")
        return dex.go_to(dex.StepRef(_SEARCH_STEP_TYPE), request)


# dex:group group-id:index-search group-label:"Index search"
# dex:explanation text:"Complete with the matching record identities."
class _Searched(dex.Step[QueryResult[SearchPage]]):
    def get_step_type(self) -> str:
        return "AlgoliaHitsFound"

    def execute(self, context: dex.Context, input: QueryResult[SearchPage]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class IndexSearchFlow(dex.Flow[IndexSearchRequest]):
    """Search one Algolia index through the static algolia connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("index search requires the static algolia connection")
        self._start = _Start()
        self._searched = _Searched()
        self._search = new_search_step(
            step_type=_SEARCH_STEP_TYPE,
            step_input_type=IndexSearchRequest,
            annotations={
                "groupId": "index-search",
                "groupLabel": "Index search",
                "explanation": "Search the connected Algolia index for matching records.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_search_input,
            searched=self._searched,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[IndexSearchRequest]:
        """Register the start, search, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._search, self._searched)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the hit page."""
        return dex.PersistenceSchema.of(_OUTCOME)
