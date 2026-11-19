# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import IAMUser, ListUsersInput, ListUsersOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_USERS_DEFINITION, PROVIDER_REJECTED

def list_users_listed(value: ListUsersOutput, failure: Failure | None = None) -> Case[ListUsersOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_users_listed, '__module__', "dex_connectors.kingsoftcloud.kingsoftcloudmock")

def list_users_provider_rejected(value: ListUsersOutput, failure: Failure | None = None) -> Case[ListUsersOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_users_provider_rejected, '__module__', "dex_connectors.kingsoftcloud.kingsoftcloudmock")

def list_users_invalid_response(value: ListUsersOutput, failure: Failure | None = None) -> Case[ListUsersOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_users_invalid_response, '__module__', "dex_connectors.kingsoftcloud.kingsoftcloudmock")

def list_users_defect(value: ListUsersOutput, failure: Failure | None = None) -> Case[ListUsersOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_users_defect, '__module__', "dex_connectors.kingsoftcloud.kingsoftcloudmock")

def list_users_retry(failure: Failure, after_seconds: float = 0) -> Case[ListUsersOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_users_retry, '__module__', "dex_connectors.kingsoftcloud.kingsoftcloudmock")

def list_users_sample_user_page() -> Case[ListUsersOutput]:
    return Case.branch_result(LISTED, ListUsersOutput(users=(IAMUser(user_id='example-user-001', user_name='example-user'),), has_more=False, next_marker=''))

setattr(list_users_sample_user_page, '__module__', "dex_connectors.kingsoftcloud.kingsoftcloudmock")


class Mock:

    def __init__(self, connection_name: str = "kingsoftcloud") -> None:
        self.list_users_script: Query[ListUsersInput, ListUsersOutput] = Query(LIST_USERS_DEFINITION)
        self.list_users_script.default(list_users_sample_user_page())
        self._connection = new_connection_with_operations(
            list_users=self.list_users_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.kingsoftcloud.kingsoftcloudmock")

__all__ = [
    "Mock",
    "list_users_listed",
    "list_users_provider_rejected",
    "list_users_invalid_response",
    "list_users_defect",
    "list_users_retry",
    "list_users_sample_user_page",
]
