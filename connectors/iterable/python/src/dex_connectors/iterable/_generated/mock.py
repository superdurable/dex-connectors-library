# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetUserInput, UserIdentity
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_USER_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_user_found(value: UserIdentity, failure: Failure | None = None) -> Case[UserIdentity]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_user_found, '__module__', "dex_connectors.iterable.iterablemock")

def get_user_not_found(value: UserIdentity, failure: Failure | None = None) -> Case[UserIdentity]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_user_not_found, '__module__', "dex_connectors.iterable.iterablemock")

def get_user_provider_rejected(value: UserIdentity, failure: Failure | None = None) -> Case[UserIdentity]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_user_provider_rejected, '__module__', "dex_connectors.iterable.iterablemock")

def get_user_invalid_response(value: UserIdentity, failure: Failure | None = None) -> Case[UserIdentity]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_user_invalid_response, '__module__', "dex_connectors.iterable.iterablemock")

def get_user_defect(value: UserIdentity, failure: Failure | None = None) -> Case[UserIdentity]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_user_defect, '__module__', "dex_connectors.iterable.iterablemock")

def get_user_retry(failure: Failure, after_seconds: float = 0) -> Case[UserIdentity]:
    return Case.retry(failure, after_seconds)

setattr(get_user_retry, '__module__', "dex_connectors.iterable.iterablemock")

def get_user_found_default() -> Case[UserIdentity]:
    return Case.branch_result(FOUND, UserIdentity(email='user@example.com', user_id='example-user-id'))

setattr(get_user_found_default, '__module__', "dex_connectors.iterable.iterablemock")


class Mock:

    def __init__(self, connection_name: str = "iterable") -> None:
        self.get_user_script: Query[GetUserInput, UserIdentity] = Query(GET_USER_DEFINITION)
        self.get_user_script.default(get_user_found_default())
        self._connection = new_connection_with_operations(
            get_user=self.get_user_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.iterable.iterablemock")

__all__ = [
    "Mock",
    "get_user_found",
    "get_user_not_found",
    "get_user_provider_rejected",
    "get_user_invalid_response",
    "get_user_defect",
    "get_user_retry",
    "get_user_found_default",
]
