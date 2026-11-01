# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable movie search Flow using the TMDB searchMovies Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.tmdb import (
    Connection,
    MoviePage,
    SearchMoviesInput,
    new_search_movies_step,
)

FLOW_TYPE = "TMDBMovieSearch"
CONNECTION_NAME = "tmdb"
_SEARCH_STEP_TYPE = "SearchMovies"
_OUTCOME = dex.Attribute("tmdb-movie-search-outcome", MoviePage)


def map_to_search_movies_input(request: SearchMoviesInput) -> SearchMoviesInput:
    """Search with the requested text and page unchanged."""
    return request


# dex:group group-id:movie-search group-label:"Movie search"
# dex:explanation text:"Check the requested page window before calling TMDB."
class _PrepareRequest(dex.Step[SearchMoviesInput]):
    def get_step_type(self) -> str:
        return "PrepareRequest"

    def execute(self, context: dex.Context, input: SearchMoviesInput) -> dex.StepDecision:
        if not input.query.strip() or not 0 <= input.page <= 500:
            return dex.force_fail("query is required and page must be 0 to 500")
        return dex.go_to(dex.StepRef(_SEARCH_STEP_TYPE), input)


# dex:group group-id:movie-search group-label:"Movie search"
# dex:explanation text:"Record the movie page and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[MoviePage]]):
    def get_step_type(self) -> str:
        return "CompleteRequest"

    def execute(self, context: dex.Context, input: QueryResult[MoviePage]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class MovieSearchFlow(dex.Flow[SearchMoviesInput]):
    """Search one page of movies through the static tmdb connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("movie search requires the static tmdb connection")
        self._prepare = _PrepareRequest()
        self._complete = _CompleteRequest()
        self._search = new_search_movies_step(
            step_type=_SEARCH_STEP_TYPE,
            step_input_type=SearchMoviesInput,
            annotations={
                "groupId": "tmdb",
                "groupLabel": "TMDB",
                "explanation": "Search TMDB for movies matching the title text.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_search_movies_input,
            searched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[SearchMoviesInput]:
        """Register request check, provider search, and completion Steps."""
        return dex.StepList.start_step(self._prepare).other_steps(self._search, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the movie page."""
        return dex.PersistenceSchema.of(_OUTCOME)
