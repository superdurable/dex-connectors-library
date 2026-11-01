# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import get_movie_default as get_movie_default
from .._generated.mock import get_movie_found as get_movie_found
from .._generated.mock import get_movie_not_found as get_movie_not_found
from .._generated.mock import search_movies_default as search_movies_default
from .._generated.mock import search_movies_provider_rejected as search_movies_provider_rejected
from .._generated.mock import search_movies_searched as search_movies_searched
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import FOUND as FOUND
from ..client import GET_MOVIE_DEFINITION as GET_MOVIE_DEFINITION
from ..client import NOT_FOUND as NOT_FOUND
from ..client import PROVIDER_REJECTED as PROVIDER_REJECTED
from ..client import SEARCH_MOVIES_DEFINITION as SEARCH_MOVIES_DEFINITION
from ..client import SEARCHED as SEARCHED
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import GetMovieInput as GetMovieInput
from ..models import Movie as Movie
from ..models import MoviePage as MoviePage
from ..models import SearchMoviesInput as SearchMoviesInput

__all__ = [
    "Mock",
    "search_movies_searched",
    "search_movies_provider_rejected",
    "search_movies_default",
    "get_movie_found",
    "get_movie_not_found",
    "get_movie_default",
]
