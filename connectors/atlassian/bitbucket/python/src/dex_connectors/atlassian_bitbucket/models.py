# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass

from ._generated.models import (
    ListPullRequestsInput as ListPullRequestsInput,
)
from ._generated.models import (
    ListRepositoriesInput as ListRepositoriesInput,
)
from ._generated.models import (
    PullRequest as PullRequest,
)
from ._generated.models import (
    Repository as Repository,
)


@dataclass(frozen=True, slots=True)
class RepositoryPage:
    """First page of repositories; next is Bitbucket's next-page URL or blank."""

    values: tuple[Repository, ...] = ()
    next: str = ""
    page: int = 0
    pagelen: int = 0


@dataclass(frozen=True, slots=True)
class PullRequestPage:
    """First page of pull requests; next is Bitbucket's next-page URL or blank."""

    values: tuple[PullRequest, ...] = ()
    next: str = ""
    page: int = 0
    pagelen: int = 0
