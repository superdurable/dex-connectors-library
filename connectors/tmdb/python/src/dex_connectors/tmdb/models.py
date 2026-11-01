# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    GetMovieInput as GetMovieInput,
)
from ._generated.models import (
    Movie as Movie,
)
from ._generated.models import MoviePage as MoviePage
from ._generated.models import (
    SearchMoviesInput as SearchMoviesInput,
)

MoviePage.__doc__ = "One page of movie matches and TMDB's page and result totals."
