# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetUserProfileInput, UserProfile
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_USER_PROFILE_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_user_profile_found(value: UserProfile, failure: Failure | None = None) -> Case[UserProfile]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_user_profile_found, '__module__', "dex_connectors.braze.brazemock")

def get_user_profile_not_found(value: UserProfile, failure: Failure | None = None) -> Case[UserProfile]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_user_profile_not_found, '__module__', "dex_connectors.braze.brazemock")

def get_user_profile_provider_rejected(value: UserProfile, failure: Failure | None = None) -> Case[UserProfile]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_user_profile_provider_rejected, '__module__', "dex_connectors.braze.brazemock")

def get_user_profile_invalid_response(value: UserProfile, failure: Failure | None = None) -> Case[UserProfile]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_user_profile_invalid_response, '__module__', "dex_connectors.braze.brazemock")

def get_user_profile_defect(value: UserProfile, failure: Failure | None = None) -> Case[UserProfile]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_user_profile_defect, '__module__', "dex_connectors.braze.brazemock")

def get_user_profile_retry(failure: Failure, after_seconds: float = 0) -> Case[UserProfile]:
    return Case.retry(failure, after_seconds)

setattr(get_user_profile_retry, '__module__', "dex_connectors.braze.brazemock")

def get_user_profile_found_default() -> Case[UserProfile]:
    return Case.branch_result(FOUND, UserProfile(external_id='sample-user-1', braze_id='sample-braze-1', first_name='Avery', last_name='Reader', email='reader@example.com'))

setattr(get_user_profile_found_default, '__module__', "dex_connectors.braze.brazemock")

def get_user_profile_user_missing() -> Case[UserProfile]:
    return Case.branch_result(NOT_FOUND, UserProfile(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getUserProfile", "Braze did not find the user."))

setattr(get_user_profile_user_missing, '__module__', "dex_connectors.braze.brazemock")


class Mock:

    def __init__(self, connection_name: str = "braze") -> None:
        self.get_user_profile_script: Query[GetUserProfileInput, UserProfile] = Query(GET_USER_PROFILE_DEFINITION)
        self.get_user_profile_script.default(get_user_profile_found_default())
        self._connection = new_connection_with_operations(
            get_user_profile=self.get_user_profile_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.braze.brazemock")

__all__ = [
    "Mock",
    "get_user_profile_found",
    "get_user_profile_not_found",
    "get_user_profile_provider_rejected",
    "get_user_profile_invalid_response",
    "get_user_profile_defect",
    "get_user_profile_retry",
    "get_user_profile_found_default",
    "get_user_profile_user_missing",
]
