# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass

@dataclass(frozen=True, slots=True)
class SearchMoviesInput:
    """Typed SearchMoviesInput value from the Go connector contract."""

    query: str = ''
    page: int = 0


setattr(SearchMoviesInput, '__module__', 'dex_connectors.tmdb.models')


@dataclass(frozen=True, slots=True)
class Movie:
    """Typed Movie value from the Go connector contract."""

    id: int = 0
    title: str = ''
    overview: str = ''
    release_date: str = ''


setattr(Movie, '__module__', 'dex_connectors.tmdb.models')


@dataclass(frozen=True, slots=True)
class MoviePage:
    """Typed MoviePage value from the Go connector contract."""

    page: int = 0
    total_pages: int = 0
    total_results: int = 0
    results: tuple[Movie, ...] | None = None


setattr(MoviePage, '__module__', 'dex_connectors.tmdb.models')


@dataclass(frozen=True, slots=True)
class GetMovieInput:
    """Typed GetMovieInput value from the Go connector contract."""

    id: int = 0


setattr(GetMovieInput, '__module__', 'dex_connectors.tmdb.models')
