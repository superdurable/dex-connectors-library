# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import SearchUsersInput, UserSearchPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, PROVIDER_REJECTED, SEARCHED, SEARCH_USERS_DEFINITION

def search_users_searched(value: UserSearchPage, failure: Failure | None = None) -> Case[UserSearchPage]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_users_searched, '__module__', "dex_connectors.amplitude.amplitudemock")

def search_users_provider_rejected(value: UserSearchPage, failure: Failure | None = None) -> Case[UserSearchPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_users_provider_rejected, '__module__', "dex_connectors.amplitude.amplitudemock")

def search_users_invalid_response(value: UserSearchPage, failure: Failure | None = None) -> Case[UserSearchPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_users_invalid_response, '__module__', "dex_connectors.amplitude.amplitudemock")

def search_users_defect(value: UserSearchPage, failure: Failure | None = None) -> Case[UserSearchPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_users_defect, '__module__', "dex_connectors.amplitude.amplitudemock")

def search_users_retry(failure: Failure, after_seconds: float = 0) -> Case[UserSearchPage]:
    return Case.retry(failure, after_seconds)

setattr(search_users_retry, '__module__', "dex_connectors.amplitude.amplitudemock")

def search_users_searched_default() -> Case[UserSearchPage]:
    return Case.branch_result(SEARCHED, UserSearchPage(user='example-user', user_ids=('example-user',), match_type='match_user_or_device_id'))

setattr(search_users_searched_default, '__module__', "dex_connectors.amplitude.amplitudemock")


class Mock:

    def __init__(self, connection_name: str = "amplitude") -> None:
        self.search_users_script: Query[SearchUsersInput, UserSearchPage] = Query(SEARCH_USERS_DEFINITION)
        self.search_users_script.default(search_users_searched_default())
        self._connection = new_connection_with_operations(
            search_users=self.search_users_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.amplitude.amplitudemock")

__all__ = [
    "Mock",
    "search_users_searched",
    "search_users_provider_rejected",
    "search_users_invalid_response",
    "search_users_defect",
    "search_users_retry",
    "search_users_searched_default",
]
