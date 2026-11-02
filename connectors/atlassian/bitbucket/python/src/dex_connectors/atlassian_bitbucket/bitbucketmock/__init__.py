# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import list_pull_requests_default_answer as list_pull_requests_default_answer
from .._generated.mock import list_pull_requests_listed as list_pull_requests_listed
from .._generated.mock import list_repositories_default_answer as list_repositories_default_answer
from .._generated.mock import list_repositories_listed as list_repositories_listed
from .._generated.mock import list_repositories_not_found as list_repositories_not_found
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import LIST_PULL_REQUESTS_DEFINITION as LIST_PULL_REQUESTS_DEFINITION
from ..client import LIST_REPOSITORIES_DEFINITION as LIST_REPOSITORIES_DEFINITION
from ..client import LISTED as LISTED
from ..client import NOT_FOUND as NOT_FOUND
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import ListPullRequestsInput as ListPullRequestsInput
from ..models import ListRepositoriesInput as ListRepositoriesInput
from ..models import PullRequest as PullRequest
from ..models import PullRequestPage as PullRequestPage
from ..models import Repository as Repository
from ..models import RepositoryPage as RepositoryPage

__all__ = [
    "Mock",
    "list_repositories_listed",
    "list_repositories_not_found",
    "list_repositories_default_answer",
    "list_pull_requests_listed",
    "list_pull_requests_default_answer",
]

Mock.__doc__ = "Script Bitbucket Queries, defaulting to their manifest answers."

list_repositories_listed.__doc__ = "Script the successful listed route."
list_repositories_not_found.__doc__ = "Script a missing workspace."
list_repositories_default_answer.__doc__ = "Return the manifest's listRepositories defaultAnswer."
list_pull_requests_listed.__doc__ = "Script the successful listed route."
list_pull_requests_default_answer.__doc__ = "Return the manifest's listPullRequests defaultAnswer."
