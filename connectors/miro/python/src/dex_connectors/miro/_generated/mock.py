# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import BoardMetadata, GetBoardInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_BOARD_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_board_found(value: BoardMetadata, failure: Failure | None = None) -> Case[BoardMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_board_found, '__module__', "dex_connectors.miro.miromock")

def get_board_not_found(value: BoardMetadata, failure: Failure | None = None) -> Case[BoardMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_board_not_found, '__module__', "dex_connectors.miro.miromock")

def get_board_provider_rejected(value: BoardMetadata, failure: Failure | None = None) -> Case[BoardMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_board_provider_rejected, '__module__', "dex_connectors.miro.miromock")

def get_board_invalid_response(value: BoardMetadata, failure: Failure | None = None) -> Case[BoardMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_board_invalid_response, '__module__', "dex_connectors.miro.miromock")

def get_board_defect(value: BoardMetadata, failure: Failure | None = None) -> Case[BoardMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_board_defect, '__module__', "dex_connectors.miro.miromock")

def get_board_retry(failure: Failure, after_seconds: float = 0) -> Case[BoardMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_board_retry, '__module__', "dex_connectors.miro.miromock")

def get_board_found_default() -> Case[BoardMetadata]:
    return Case.branch_result(FOUND, BoardMetadata(id='uXjVOD6LSME=', name='Example board'))

setattr(get_board_found_default, '__module__', "dex_connectors.miro.miromock")


class Mock:

    def __init__(self, connection_name: str = "miro") -> None:
        self.get_board_script: Query[GetBoardInput, BoardMetadata] = Query(GET_BOARD_DEFINITION)
        self.get_board_script.default(get_board_found_default())
        self._connection = new_connection_with_operations(
            get_board=self.get_board_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.miro.miromock")

__all__ = [
    "Mock",
    "get_board_found",
    "get_board_not_found",
    "get_board_provider_rejected",
    "get_board_invalid_response",
    "get_board_defect",
    "get_board_retry",
    "get_board_found_default",
]
