# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetMovieInput, Movie, MoviePage, SearchMoviesInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_MOVIE_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, SEARCHED, SEARCH_MOVIES_DEFINITION

def search_movies_searched(value: MoviePage, failure: Failure | None = None) -> Case[MoviePage]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_movies_searched, '__module__', "dex_connectors.tmdb.tmdbmock")

def search_movies_provider_rejected(value: MoviePage, failure: Failure | None = None) -> Case[MoviePage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_movies_provider_rejected, '__module__', "dex_connectors.tmdb.tmdbmock")

def search_movies_invalid_response(value: MoviePage, failure: Failure | None = None) -> Case[MoviePage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_movies_invalid_response, '__module__', "dex_connectors.tmdb.tmdbmock")

def search_movies_defect(value: MoviePage, failure: Failure | None = None) -> Case[MoviePage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_movies_defect, '__module__', "dex_connectors.tmdb.tmdbmock")

def search_movies_retry(failure: Failure, after_seconds: float = 0) -> Case[MoviePage]:
    return Case.retry(failure, after_seconds)

setattr(search_movies_retry, '__module__', "dex_connectors.tmdb.tmdbmock")

def search_movies_default() -> Case[MoviePage]:
    return Case.branch_result(SEARCHED, MoviePage(page=1, total_pages=1, total_results=1, results=(Movie(id=343611, title='Jack Reacher: Never Go Back', overview='A thriller.', release_date='2016-10-19'),)))

setattr(search_movies_default, '__module__', "dex_connectors.tmdb.tmdbmock")

def get_movie_found(value: Movie, failure: Failure | None = None) -> Case[Movie]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_movie_found, '__module__', "dex_connectors.tmdb.tmdbmock")

def get_movie_not_found(value: Movie, failure: Failure | None = None) -> Case[Movie]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_movie_not_found, '__module__', "dex_connectors.tmdb.tmdbmock")

def get_movie_provider_rejected(value: Movie, failure: Failure | None = None) -> Case[Movie]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_movie_provider_rejected, '__module__', "dex_connectors.tmdb.tmdbmock")

def get_movie_invalid_response(value: Movie, failure: Failure | None = None) -> Case[Movie]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_movie_invalid_response, '__module__', "dex_connectors.tmdb.tmdbmock")

def get_movie_defect(value: Movie, failure: Failure | None = None) -> Case[Movie]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_movie_defect, '__module__', "dex_connectors.tmdb.tmdbmock")

def get_movie_retry(failure: Failure, after_seconds: float = 0) -> Case[Movie]:
    return Case.retry(failure, after_seconds)

setattr(get_movie_retry, '__module__', "dex_connectors.tmdb.tmdbmock")

def get_movie_default() -> Case[Movie]:
    return Case.branch_result(FOUND, Movie(id=343611, title='Jack Reacher: Never Go Back', overview='A thriller.', release_date='2016-10-19'))

setattr(get_movie_default, '__module__', "dex_connectors.tmdb.tmdbmock")


class Mock:

    def __init__(self, connection_name: str = "tmdb") -> None:
        self.search_movies_script: Query[SearchMoviesInput, MoviePage] = Query(SEARCH_MOVIES_DEFINITION)
        self.search_movies_script.default(search_movies_default())
        self.get_movie_script: Query[GetMovieInput, Movie] = Query(GET_MOVIE_DEFINITION)
        self.get_movie_script.default(get_movie_default())
        self._connection = new_connection_with_operations(
            search_movies=self.search_movies_script,
            get_movie=self.get_movie_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.tmdb.tmdbmock")

__all__ = [
    "Mock",
    "search_movies_searched",
    "search_movies_provider_rejected",
    "search_movies_invalid_response",
    "search_movies_defect",
    "search_movies_retry",
    "search_movies_default",
    "get_movie_found",
    "get_movie_not_found",
    "get_movie_provider_rejected",
    "get_movie_invalid_response",
    "get_movie_defect",
    "get_movie_retry",
    "get_movie_default",
]
