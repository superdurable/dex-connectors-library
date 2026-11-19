# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetUserDetailInput, UserDetail
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_USER_DETAIL_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_user_detail_found(value: UserDetail, failure: Failure | None = None) -> Case[UserDetail]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_user_detail_found, '__module__', "dex_connectors.easemob.easemobmock")

def get_user_detail_provider_rejected(value: UserDetail, failure: Failure | None = None) -> Case[UserDetail]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_user_detail_provider_rejected, '__module__', "dex_connectors.easemob.easemobmock")

def get_user_detail_invalid_response(value: UserDetail, failure: Failure | None = None) -> Case[UserDetail]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_user_detail_invalid_response, '__module__', "dex_connectors.easemob.easemobmock")

def get_user_detail_defect(value: UserDetail, failure: Failure | None = None) -> Case[UserDetail]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_user_detail_defect, '__module__', "dex_connectors.easemob.easemobmock")

def get_user_detail_retry(failure: Failure, after_seconds: float = 0) -> Case[UserDetail]:
    return Case.retry(failure, after_seconds)

setattr(get_user_detail_retry, '__module__', "dex_connectors.easemob.easemobmock")

def get_user_detail_found_default() -> Case[UserDetail]:
    return Case.branch_result(FOUND, UserDetail(user_id='user1', user_uuid='0ffe2d80-0000-0000-8d66-279e3e1c214b', created_at_milliseconds=1542795196504, modified_at_milliseconds=1542795196504, activated=True, nickname='Sample User'))

setattr(get_user_detail_found_default, '__module__', "dex_connectors.easemob.easemobmock")


class Mock:

    def __init__(self, connection_name: str = "easemob") -> None:
        self.get_user_detail_script: Query[GetUserDetailInput, UserDetail] = Query(GET_USER_DETAIL_DEFINITION)
        self.get_user_detail_script.default(get_user_detail_found_default())
        self._connection = new_connection_with_operations(
            get_user_detail=self.get_user_detail_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.easemob.easemobmock")

__all__ = [
    "Mock",
    "get_user_detail_found",
    "get_user_detail_provider_rejected",
    "get_user_detail_invalid_response",
    "get_user_detail_defect",
    "get_user_detail_retry",
    "get_user_detail_found_default",
]
