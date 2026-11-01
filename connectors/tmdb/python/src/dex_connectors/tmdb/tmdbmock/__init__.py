# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed TMDB scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import (
    CONNECTOR_ID,
    FOUND,
    GET_MOVIE_DEFINITION,
    NOT_FOUND,
    PROVIDER_REJECTED,
    SEARCH_MOVIES_DEFINITION,
    SEARCHED,
)
from ..connection import Connection, new_connection_with_operations
from ..models import GetMovieInput, Movie, MoviePage, SearchMoviesInput

_DEFAULT_MOVIE = Movie(343611, "Jack Reacher: Never Go Back", "A thriller.", "2016-10-19")


def search_movies_searched(value: MoviePage, failure: Failure | None = None) -> Case[MoviePage]:
    """Script the successful searched route."""
    return Case.branch_result(SEARCHED, value, failure)


def search_movies_provider_rejected(value: MoviePage, failure: Failure | None = None) -> Case[MoviePage]:
    """Script a definite provider rejection of the search."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)


def search_movies_default() -> Case[MoviePage]:
    """Return the manifest's searchMoviesDefault answer."""
    return search_movies_searched(MoviePage(1, 1, 1, (_DEFAULT_MOVIE,)))


def get_movie_found(value: Movie, failure: Failure | None = None) -> Case[Movie]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_movie_not_found(value: Movie, failure: Failure | None = None) -> Case[Movie]:
    """Script a missing movie."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_movie_default() -> Case[Movie]:
    """Return the manifest's getMovieDefault answer."""
    return get_movie_found(_DEFAULT_MOVIE)


class Mock:
    """Script TMDB operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "tmdb") -> None:
        self.search_movies_script: Query[SearchMoviesInput, MoviePage] = Query(SEARCH_MOVIES_DEFINITION)
        self.search_movies_script.default(search_movies_default())
        self.get_movie_script: Query[GetMovieInput, Movie] = Query(GET_MOVIE_DEFINITION)
        self.get_movie_script.default(get_movie_default())
        self._connection = new_connection_with_operations(
            self.search_movies_script, self.get_movie_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "search_movies_searched",
    "search_movies_provider_rejected",
    "search_movies_default",
    "get_movie_found",
    "get_movie_not_found",
    "get_movie_default",
]
