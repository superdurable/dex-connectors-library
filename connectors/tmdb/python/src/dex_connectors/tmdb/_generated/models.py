# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return value
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# SearchMoviesInput searches movie titles.
@dataclass(frozen=True, slots=True)
class SearchMoviesInput:
    # Query is the provider search expression.
    query: str = ''
    # Page is the one-based movie search page.
    page: int = 0


setattr(SearchMoviesInput, '__module__', 'dex_connectors.tmdb.models')


# Movie contains selected public movie fields.
@dataclass(frozen=True, slots=True)
class Movie:
    # ID is the provider's stable resource identifier.
    id: int = 0
    # Title is the provider's display title.
    title: str = ''
    # Overview is the movie summary text.
    overview: str = ''
    # ReleaseDate is the movie release date.
    release_date: str = ''


setattr(Movie, '__module__', 'dex_connectors.tmdb.models')


# MoviePage is one search results page.
@dataclass(frozen=True, slots=True)
class MoviePage:
    # Page is the one-based movie search page.
    page: int = 0
    # TotalPages is the number of matching search pages.
    total_pages: int = 0
    # TotalResults is the number of matching movies.
    total_results: int = 0
    # Results contains one movie search page.
    results: tuple[Movie, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'results', _replace_nil_slices(self.results, 1))


MoviePage.__annotations__['results'] = tuple[Movie, ...] | None
setattr(MoviePage, '__module__', 'dex_connectors.tmdb.models')


# GetMovieInput identifies a movie by TMDB ID.
@dataclass(frozen=True, slots=True)
class GetMovieInput:
    # ID is the provider's stable resource identifier.
    id: int = 0


setattr(GetMovieInput, '__module__', 'dex_connectors.tmdb.models')
