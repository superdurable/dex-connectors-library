# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListRoomsInput, ListRoomsOutput, Room
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, LIST_ROOMS_DEFINITION, PROVIDER_REJECTED

def list_rooms_found(value: ListRoomsOutput, failure: Failure | None = None) -> Case[ListRoomsOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(list_rooms_found, '__module__', "dex_connectors.likelic.likelicmock")

def list_rooms_provider_rejected(value: ListRoomsOutput, failure: Failure | None = None) -> Case[ListRoomsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_rooms_provider_rejected, '__module__', "dex_connectors.likelic.likelicmock")

def list_rooms_invalid_response(value: ListRoomsOutput, failure: Failure | None = None) -> Case[ListRoomsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_rooms_invalid_response, '__module__', "dex_connectors.likelic.likelicmock")

def list_rooms_defect(value: ListRoomsOutput, failure: Failure | None = None) -> Case[ListRoomsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_rooms_defect, '__module__', "dex_connectors.likelic.likelicmock")

def list_rooms_retry(failure: Failure, after_seconds: float = 0) -> Case[ListRoomsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_rooms_retry, '__module__', "dex_connectors.likelic.likelicmock")

def list_rooms_one_room() -> Case[ListRoomsOutput]:
    return Case.branch_result(FOUND, ListRoomsOutput(page=1, total=1, rooms=(Room(channel='wework', instance_id=3, room_id='room-123', push_state='ready'),)))

setattr(list_rooms_one_room, '__module__', "dex_connectors.likelic.likelicmock")

def list_rooms_no_rooms() -> Case[ListRoomsOutput]:
    return Case.branch_result(FOUND, ListRoomsOutput(page=1, total=0, rooms=()))

setattr(list_rooms_no_rooms, '__module__', "dex_connectors.likelic.likelicmock")


class Mock:

    def __init__(self, connection_name: str = "likelic") -> None:
        self.list_rooms_script: Query[ListRoomsInput, ListRoomsOutput] = Query(LIST_ROOMS_DEFINITION)
        self.list_rooms_script.default(list_rooms_one_room())
        self._connection = new_connection_with_operations(
            list_rooms=self.list_rooms_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.likelic.likelicmock")

__all__ = [
    "Mock",
    "list_rooms_found",
    "list_rooms_provider_rejected",
    "list_rooms_invalid_response",
    "list_rooms_defect",
    "list_rooms_retry",
    "list_rooms_one_room",
    "list_rooms_no_rooms",
]
