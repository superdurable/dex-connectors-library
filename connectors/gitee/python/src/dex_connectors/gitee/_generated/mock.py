# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetRepositoryInput, GetRepositoryOutput, RepositoryMetadata
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_REPOSITORY_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_repository_found(value: GetRepositoryOutput, failure: Failure | None = None) -> Case[GetRepositoryOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_repository_found, '__module__', "dex_connectors.gitee.giteemock")

def get_repository_not_found(value: GetRepositoryOutput, failure: Failure | None = None) -> Case[GetRepositoryOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_repository_not_found, '__module__', "dex_connectors.gitee.giteemock")

def get_repository_provider_rejected(value: GetRepositoryOutput, failure: Failure | None = None) -> Case[GetRepositoryOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_repository_provider_rejected, '__module__', "dex_connectors.gitee.giteemock")

def get_repository_invalid_response(value: GetRepositoryOutput, failure: Failure | None = None) -> Case[GetRepositoryOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_repository_invalid_response, '__module__', "dex_connectors.gitee.giteemock")

def get_repository_defect(value: GetRepositoryOutput, failure: Failure | None = None) -> Case[GetRepositoryOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_repository_defect, '__module__', "dex_connectors.gitee.giteemock")

def get_repository_retry(failure: Failure, after_seconds: float = 0) -> Case[GetRepositoryOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_repository_retry, '__module__', "dex_connectors.gitee.giteemock")

def get_repository_public_repository() -> Case[GetRepositoryOutput]:
    return Case.branch_result(FOUND, GetRepositoryOutput(repository=RepositoryMetadata(id=101, full_name='open-source/demo', name='demo', description='Example repository', html_url='https://gitee.com/open-source/demo', private=False, default_branch='master', stars_count=5, forks_count=2)))

setattr(get_repository_public_repository, '__module__', "dex_connectors.gitee.giteemock")


class Mock:

    def __init__(self, connection_name: str = "gitee") -> None:
        self.get_repository_script: Query[GetRepositoryInput, GetRepositoryOutput] = Query(GET_REPOSITORY_DEFINITION)
        self.get_repository_script.default(get_repository_public_repository())
        self._connection = new_connection_with_operations(
            get_repository=self.get_repository_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.gitee.giteemock")

__all__ = [
    "Mock",
    "get_repository_found",
    "get_repository_not_found",
    "get_repository_provider_rejected",
    "get_repository_invalid_response",
    "get_repository_defect",
    "get_repository_retry",
    "get_repository_public_repository",
]
