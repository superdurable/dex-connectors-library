# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed TMDB movie searches, pages, and movie records."""

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class SearchMoviesInput:
    """Title search text and page; page zero means TMDB's first page, at most 500."""

    query: str = ""
    page: int = 0


@dataclass(frozen=True, slots=True)
class GetMovieInput:
    """Positive TMDB movie ID, such as 343611."""

    id: int = 0


@dataclass(frozen=True, slots=True)
class Movie:
    """Movie summary; release_date is TMDB's YYYY-MM-DD text or blank."""

    id: int = 0
    title: str = ""
    overview: str = ""
    release_date: str = ""


@dataclass(frozen=True, slots=True)
class MoviePage:
    """One page of movie matches and TMDB's page and result totals."""

    page: int = 0
    total_pages: int = 0
    total_results: int = 0
    results: tuple[Movie, ...] = ()
