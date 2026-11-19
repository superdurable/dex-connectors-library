# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListRoomsInput, ListRoomsOutput, RoomSummary
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_ROOMS_DEFINITION, PROVIDER_REJECTED

def list_rooms_listed(value: ListRoomsOutput, failure: Failure | None = None) -> Case[ListRoomsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_rooms_listed, '__module__', "dex_connectors.quanshi.quanshimock")

def list_rooms_provider_rejected(value: ListRoomsOutput, failure: Failure | None = None) -> Case[ListRoomsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_rooms_provider_rejected, '__module__', "dex_connectors.quanshi.quanshimock")

def list_rooms_invalid_response(value: ListRoomsOutput, failure: Failure | None = None) -> Case[ListRoomsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_rooms_invalid_response, '__module__', "dex_connectors.quanshi.quanshimock")

def list_rooms_defect(value: ListRoomsOutput, failure: Failure | None = None) -> Case[ListRoomsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_rooms_defect, '__module__', "dex_connectors.quanshi.quanshimock")

def list_rooms_retry(failure: Failure, after_seconds: float = 0) -> Case[ListRoomsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_rooms_retry, '__module__', "dex_connectors.quanshi.quanshimock")

def list_rooms_sample_rooms() -> Case[ListRoomsOutput]:
    return Case.branch_result(LISTED, ListRoomsOutput(rooms=(RoomSummary(id=10935, name='Example Room', capacity=20, status=1),)))

setattr(list_rooms_sample_rooms, '__module__', "dex_connectors.quanshi.quanshimock")


class Mock:

    def __init__(self, connection_name: str = "quanshi") -> None:
        self.list_rooms_script: Query[ListRoomsInput, ListRoomsOutput] = Query(LIST_ROOMS_DEFINITION)
        self.list_rooms_script.default(list_rooms_sample_rooms())
        self._connection = new_connection_with_operations(
            list_rooms=self.list_rooms_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.quanshi.quanshimock")

__all__ = [
    "Mock",
    "list_rooms_listed",
    "list_rooms_provider_rejected",
    "list_rooms_invalid_response",
    "list_rooms_defect",
    "list_rooms_retry",
    "list_rooms_sample_rooms",
]
