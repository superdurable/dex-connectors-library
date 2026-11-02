# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListPullRequestsInput, ListRepositoriesInput, PullRequest, PullRequestPage, Repository, RepositoryPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_PULL_REQUESTS_DEFINITION, LIST_REPOSITORIES_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_repositories_listed(value: RepositoryPage, failure: Failure | None = None) -> Case[RepositoryPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_repositories_listed, '__module__', "dex_connectors.atlassian_bitbucket.bitbucketmock")

def list_repositories_not_found(value: RepositoryPage, failure: Failure | None = None) -> Case[RepositoryPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_repositories_not_found, '__module__', "dex_connectors.atlassian_bitbucket.bitbucketmock")

def list_repositories_provider_rejected(value: RepositoryPage, failure: Failure | None = None) -> Case[RepositoryPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_repositories_provider_rejected, '__module__', "dex_connectors.atlassian_bitbucket.bitbucketmock")

def list_repositories_invalid_response(value: RepositoryPage, failure: Failure | None = None) -> Case[RepositoryPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_repositories_invalid_response, '__module__', "dex_connectors.atlassian_bitbucket.bitbucketmock")

def list_repositories_defect(value: RepositoryPage, failure: Failure | None = None) -> Case[RepositoryPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_repositories_defect, '__module__', "dex_connectors.atlassian_bitbucket.bitbucketmock")

def list_repositories_retry(failure: Failure, after_seconds: float = 0) -> Case[RepositoryPage]:
    return Case.retry(failure, after_seconds)

setattr(list_repositories_retry, '__module__', "dex_connectors.atlassian_bitbucket.bitbucketmock")

def list_repositories_default_answer() -> Case[RepositoryPage]:
    return Case.branch_result(LISTED, RepositoryPage(values=(Repository(uuid='{a1b2c3d4-1234-4567-89ab-1234567890ab}', name='Meridian API', full_name='meridian/api', is_private=True),), page=1, pagelen=10))

setattr(list_repositories_default_answer, '__module__', "dex_connectors.atlassian_bitbucket.bitbucketmock")

def list_pull_requests_listed(value: PullRequestPage, failure: Failure | None = None) -> Case[PullRequestPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_pull_requests_listed, '__module__', "dex_connectors.atlassian_bitbucket.bitbucketmock")

def list_pull_requests_not_found(value: PullRequestPage, failure: Failure | None = None) -> Case[PullRequestPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_pull_requests_not_found, '__module__', "dex_connectors.atlassian_bitbucket.bitbucketmock")

def list_pull_requests_provider_rejected(value: PullRequestPage, failure: Failure | None = None) -> Case[PullRequestPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_pull_requests_provider_rejected, '__module__', "dex_connectors.atlassian_bitbucket.bitbucketmock")

def list_pull_requests_invalid_response(value: PullRequestPage, failure: Failure | None = None) -> Case[PullRequestPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_pull_requests_invalid_response, '__module__', "dex_connectors.atlassian_bitbucket.bitbucketmock")

def list_pull_requests_defect(value: PullRequestPage, failure: Failure | None = None) -> Case[PullRequestPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_pull_requests_defect, '__module__', "dex_connectors.atlassian_bitbucket.bitbucketmock")

def list_pull_requests_retry(failure: Failure, after_seconds: float = 0) -> Case[PullRequestPage]:
    return Case.retry(failure, after_seconds)

setattr(list_pull_requests_retry, '__module__', "dex_connectors.atlassian_bitbucket.bitbucketmock")

def list_pull_requests_default_answer() -> Case[PullRequestPage]:
    return Case.branch_result(LISTED, PullRequestPage(values=(PullRequest(id=7, title='Add client tests', state='OPEN'),), page=1, pagelen=10))

setattr(list_pull_requests_default_answer, '__module__', "dex_connectors.atlassian_bitbucket.bitbucketmock")


class Mock:

    def __init__(self, connection_name: str = "bitbucket") -> None:
        self.list_repositories_script: Query[ListRepositoriesInput, RepositoryPage] = Query(LIST_REPOSITORIES_DEFINITION)
        self.list_repositories_script.default(list_repositories_default_answer())
        self.list_pull_requests_script: Query[ListPullRequestsInput, PullRequestPage] = Query(LIST_PULL_REQUESTS_DEFINITION)
        self.list_pull_requests_script.default(list_pull_requests_default_answer())
        self._connection = new_connection_with_operations(
            list_repositories=self.list_repositories_script,
            list_pull_requests=self.list_pull_requests_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.atlassian_bitbucket.bitbucketmock")

__all__ = [
    "Mock",
    "list_repositories_listed",
    "list_repositories_not_found",
    "list_repositories_provider_rejected",
    "list_repositories_invalid_response",
    "list_repositories_defect",
    "list_repositories_retry",
    "list_repositories_default_answer",
    "list_pull_requests_listed",
    "list_pull_requests_not_found",
    "list_pull_requests_provider_rejected",
    "list_pull_requests_invalid_response",
    "list_pull_requests_defect",
    "list_pull_requests_retry",
    "list_pull_requests_default_answer",
]
