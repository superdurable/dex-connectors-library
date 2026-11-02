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

SearchMoviesInput.__doc__ = "Title search text and page; page zero means TMDB's first page, at most 500."
GetMovieInput.__doc__ = "Positive TMDB movie ID, such as 343611."
Movie.__doc__ = "Movie summary; release_date is TMDB's YYYY-MM-DD text or blank."
