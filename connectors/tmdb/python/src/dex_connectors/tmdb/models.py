# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass

from ._generated.models import (
    GetMovieInput as GetMovieInput,
)
from ._generated.models import (
    Movie as Movie,
)
from ._generated.models import (
    SearchMoviesInput as SearchMoviesInput,
)


@dataclass(frozen=True, slots=True)
class MoviePage:
    """One page of movie matches and TMDB's page and result totals."""

    page: int = 0
    total_pages: int = 0
    total_results: int = 0
    results: tuple[Movie, ...] = ()
