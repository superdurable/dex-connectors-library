# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetRoomUserCountInput, GetRoomUserCountOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_ROOM_USER_COUNT_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_room_user_count_found(value: GetRoomUserCountOutput, failure: Failure | None = None) -> Case[GetRoomUserCountOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_room_user_count_found, '__module__', "dex_connectors.zego.zegomock")

def get_room_user_count_provider_rejected(value: GetRoomUserCountOutput, failure: Failure | None = None) -> Case[GetRoomUserCountOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_room_user_count_provider_rejected, '__module__', "dex_connectors.zego.zegomock")

def get_room_user_count_invalid_response(value: GetRoomUserCountOutput, failure: Failure | None = None) -> Case[GetRoomUserCountOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_room_user_count_invalid_response, '__module__', "dex_connectors.zego.zegomock")

def get_room_user_count_defect(value: GetRoomUserCountOutput, failure: Failure | None = None) -> Case[GetRoomUserCountOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_room_user_count_defect, '__module__', "dex_connectors.zego.zegomock")

def get_room_user_count_retry(failure: Failure, after_seconds: float = 0) -> Case[GetRoomUserCountOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_room_user_count_retry, '__module__', "dex_connectors.zego.zegomock")

def get_room_user_count_two_members() -> Case[GetRoomUserCountOutput]:
    return Case.branch_result(FOUND, GetRoomUserCountOutput(count=2))

setattr(get_room_user_count_two_members, '__module__', "dex_connectors.zego.zegomock")


class Mock:

    def __init__(self, connection_name: str = "zego") -> None:
        self.get_room_user_count_script: Query[GetRoomUserCountInput, GetRoomUserCountOutput] = Query(GET_ROOM_USER_COUNT_DEFINITION)
        self.get_room_user_count_script.default(get_room_user_count_two_members())
        self._connection = new_connection_with_operations(
            get_room_user_count=self.get_room_user_count_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.zego.zegomock")

__all__ = [
    "Mock",
    "get_room_user_count_found",
    "get_room_user_count_provider_rejected",
    "get_room_user_count_invalid_response",
    "get_room_user_count_defect",
    "get_room_user_count_retry",
    "get_room_user_count_two_members",
]
