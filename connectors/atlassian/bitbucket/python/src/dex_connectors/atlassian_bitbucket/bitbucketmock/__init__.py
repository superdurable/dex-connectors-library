# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Bitbucket scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import (
    CONNECTOR_ID,
    LIST_PULL_REQUESTS_DEFINITION,
    LIST_REPOSITORIES_DEFINITION,
    LISTED,
    NOT_FOUND,
)
from ..connection import Connection, new_connection_with_operations
from ..models import (
    ListPullRequestsInput,
    ListRepositoriesInput,
    PullRequest,
    PullRequestPage,
    Repository,
    RepositoryPage,
)


def list_repositories_listed(value: RepositoryPage, failure: Failure | None = None) -> Case[RepositoryPage]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_repositories_not_found(
    value: RepositoryPage, failure: Failure | None = None
) -> Case[RepositoryPage]:
    """Script a missing workspace."""
    return Case.branch_result(NOT_FOUND, value, failure)


def list_repositories_default_answer() -> Case[RepositoryPage]:
    """Return the manifest's listRepositories defaultAnswer."""
    repository = Repository("{a1b2c3d4-1234-4567-89ab-1234567890ab}", "Meridian API", "meridian/api", True)
    return list_repositories_listed(RepositoryPage((repository,), "", 1, 10))


def list_pull_requests_listed(
    value: PullRequestPage, failure: Failure | None = None
) -> Case[PullRequestPage]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_pull_requests_default_answer() -> Case[PullRequestPage]:
    """Return the manifest's listPullRequests defaultAnswer."""
    return list_pull_requests_listed(
        PullRequestPage((PullRequest(7, "Add client tests", "OPEN"),), "", 1, 10)
    )


class Mock:
    """Script Bitbucket Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "bitbucket") -> None:
        self.list_repositories_script: Query[ListRepositoriesInput, RepositoryPage] = Query(
            LIST_REPOSITORIES_DEFINITION
        )
        self.list_repositories_script.default(list_repositories_default_answer())
        self.list_pull_requests_script: Query[ListPullRequestsInput, PullRequestPage] = Query(
            LIST_PULL_REQUESTS_DEFINITION
        )
        self.list_pull_requests_script.default(list_pull_requests_default_answer())
        self._connection = new_connection_with_operations(
            self.list_repositories_script,
            self.list_pull_requests_script,
            ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "list_repositories_listed",
    "list_repositories_not_found",
    "list_repositories_default_answer",
    "list_pull_requests_listed",
    "list_pull_requests_default_answer",
]
