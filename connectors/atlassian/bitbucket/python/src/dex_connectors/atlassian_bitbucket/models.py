# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    ListPullRequestsInput as ListPullRequestsInput,
)
from ._generated.models import (
    ListRepositoriesInput as ListRepositoriesInput,
)
from ._generated.models import (
    PullRequest as PullRequest,
)
from ._generated.models import PullRequestPage as PullRequestPage
from ._generated.models import (
    Repository as Repository,
)
from ._generated.models import RepositoryPage as RepositoryPage

RepositoryPage.__doc__ = "First page of repositories; next is Bitbucket's next-page URL or blank."
PullRequestPage.__doc__ = "First page of pull requests; next is Bitbucket's next-page URL or blank."
