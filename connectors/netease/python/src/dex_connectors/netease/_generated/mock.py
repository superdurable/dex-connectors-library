# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetUserProfileInput, GetUserProfileOutput
from .definitions import CONNECTOR_ID, DEFECT, FETCHED, GET_USER_PROFILE_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_user_profile_fetched(value: GetUserProfileOutput, failure: Failure | None = None) -> Case[GetUserProfileOutput]:
    return Case.branch_result(FETCHED, value, failure)

setattr(get_user_profile_fetched, '__module__', "dex_connectors.netease.neteasemock")

def get_user_profile_not_found(value: GetUserProfileOutput, failure: Failure | None = None) -> Case[GetUserProfileOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_user_profile_not_found, '__module__', "dex_connectors.netease.neteasemock")

def get_user_profile_provider_rejected(value: GetUserProfileOutput, failure: Failure | None = None) -> Case[GetUserProfileOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_user_profile_provider_rejected, '__module__', "dex_connectors.netease.neteasemock")

def get_user_profile_invalid_response(value: GetUserProfileOutput, failure: Failure | None = None) -> Case[GetUserProfileOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_user_profile_invalid_response, '__module__', "dex_connectors.netease.neteasemock")

def get_user_profile_defect(value: GetUserProfileOutput, failure: Failure | None = None) -> Case[GetUserProfileOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_user_profile_defect, '__module__', "dex_connectors.netease.neteasemock")

def get_user_profile_retry(failure: Failure, after_seconds: float = 0) -> Case[GetUserProfileOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_user_profile_retry, '__module__', "dex_connectors.netease.neteasemock")

def get_user_profile_sample_user() -> Case[GetUserProfileOutput]:
    return Case.branch_result(FETCHED, GetUserProfileOutput(account_id='sample-user', name='Sample User'))

setattr(get_user_profile_sample_user, '__module__', "dex_connectors.netease.neteasemock")


class Mock:

    def __init__(self, connection_name: str = "netease") -> None:
        self.get_user_profile_script: Query[GetUserProfileInput, GetUserProfileOutput] = Query(GET_USER_PROFILE_DEFINITION)
        self.get_user_profile_script.default(get_user_profile_sample_user())
        self._connection = new_connection_with_operations(
            get_user_profile=self.get_user_profile_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.netease.neteasemock")

__all__ = [
    "Mock",
    "get_user_profile_fetched",
    "get_user_profile_not_found",
    "get_user_profile_provider_rejected",
    "get_user_profile_invalid_response",
    "get_user_profile_defect",
    "get_user_profile_retry",
    "get_user_profile_sample_user",
]
