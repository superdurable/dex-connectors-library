# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AuthorizedUser, GetAuthorizedUserInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_AUTHORIZED_USER_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_authorized_user_found(value: AuthorizedUser, failure: Failure | None = None) -> Case[AuthorizedUser]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_authorized_user_found, '__module__', "dex_connectors.bilibili.bilibilimock")

def get_authorized_user_provider_rejected(value: AuthorizedUser, failure: Failure | None = None) -> Case[AuthorizedUser]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_authorized_user_provider_rejected, '__module__', "dex_connectors.bilibili.bilibilimock")

def get_authorized_user_invalid_response(value: AuthorizedUser, failure: Failure | None = None) -> Case[AuthorizedUser]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_authorized_user_invalid_response, '__module__', "dex_connectors.bilibili.bilibilimock")

def get_authorized_user_defect(value: AuthorizedUser, failure: Failure | None = None) -> Case[AuthorizedUser]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_authorized_user_defect, '__module__', "dex_connectors.bilibili.bilibilimock")

def get_authorized_user_retry(failure: Failure, after_seconds: float = 0) -> Case[AuthorizedUser]:
    return Case.retry(failure, after_seconds)

setattr(get_authorized_user_retry, '__module__', "dex_connectors.bilibili.bilibilimock")

def get_authorized_user_found_default() -> Case[AuthorizedUser]:
    return Case.branch_result(FOUND, AuthorizedUser(open_id='fc9899b46ff443cea38190d355d49f3a', name='sample_creator'))

setattr(get_authorized_user_found_default, '__module__', "dex_connectors.bilibili.bilibilimock")


class Mock:

    def __init__(self, connection_name: str = "bilibili") -> None:
        self.get_authorized_user_script: Query[GetAuthorizedUserInput, AuthorizedUser] = Query(GET_AUTHORIZED_USER_DEFINITION)
        self.get_authorized_user_script.default(get_authorized_user_found_default())
        self._connection = new_connection_with_operations(
            get_authorized_user=self.get_authorized_user_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.bilibili.bilibilimock")

__all__ = [
    "Mock",
    "get_authorized_user_found",
    "get_authorized_user_provider_rejected",
    "get_authorized_user_invalid_response",
    "get_authorized_user_defect",
    "get_authorized_user_retry",
    "get_authorized_user_found_default",
]
