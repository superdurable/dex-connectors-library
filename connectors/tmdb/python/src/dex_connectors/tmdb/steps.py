# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public operation-specific Step factories from connector.yaml."""

from collections.abc import Callable as Callable
from collections.abc import Mapping as Mapping
from typing import TypeVar as TypeVar

import dex as dex
from dex_connectors.sdk import QueryResult as QueryResult
from dex_connectors.sdk import QueryStep as QueryStep
from dex_connectors.sdk import QueryStepConfig as QueryStepConfig
from dex_connectors.sdk import StepAnnotations as StepAnnotations

from ._generated.steps import (
    StepInput as StepInput,
)
from ._generated.steps import (
    new_get_movie_step as new_get_movie_step,
)
from ._generated.steps import (
    new_search_movies_step as new_search_movies_step,
)
from .connection import Connection as Connection
from .models import GetMovieInput as GetMovieInput
from .models import Movie as Movie
from .models import MoviePage as MoviePage
from .models import SearchMoviesInput as SearchMoviesInput

BranchTarget = TypeVar("BranchTarget")

new_search_movies_step.__doc__ = (
    "Create the typed movie search Step; unwired optional outcomes fail the Flow."
)
new_get_movie_step.__doc__ = "Create the typed movie lookup Step; unwired optional outcomes fail the Flow."
