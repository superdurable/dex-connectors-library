# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListPinsInput, Pin, PinsPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_PINS_DEFINITION, PROVIDER_REJECTED

def list_pins_listed(value: PinsPage, failure: Failure | None = None) -> Case[PinsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_pins_listed, '__module__', "dex_connectors.pinterest.pinterestmock")

def list_pins_provider_rejected(value: PinsPage, failure: Failure | None = None) -> Case[PinsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_pins_provider_rejected, '__module__', "dex_connectors.pinterest.pinterestmock")

def list_pins_invalid_response(value: PinsPage, failure: Failure | None = None) -> Case[PinsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_pins_invalid_response, '__module__', "dex_connectors.pinterest.pinterestmock")

def list_pins_defect(value: PinsPage, failure: Failure | None = None) -> Case[PinsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_pins_defect, '__module__', "dex_connectors.pinterest.pinterestmock")

def list_pins_retry(failure: Failure, after_seconds: float = 0) -> Case[PinsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_pins_retry, '__module__', "dex_connectors.pinterest.pinterestmock")

def list_pins_listed_default() -> Case[PinsPage]:
    return Case.branch_result(LISTED, PinsPage(pins=(Pin(id='123456789', board_id='987654321', title='Example Pin'),), next_bookmark=''))

setattr(list_pins_listed_default, '__module__', "dex_connectors.pinterest.pinterestmock")


class Mock:

    def __init__(self, connection_name: str = "pinterest") -> None:
        self.list_pins_script: Query[ListPinsInput, PinsPage] = Query(LIST_PINS_DEFINITION)
        self.list_pins_script.default(list_pins_listed_default())
        self._connection = new_connection_with_operations(
            list_pins=self.list_pins_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.pinterest.pinterestmock")

__all__ = [
    "Mock",
    "list_pins_listed",
    "list_pins_provider_rejected",
    "list_pins_invalid_response",
    "list_pins_defect",
    "list_pins_retry",
    "list_pins_listed_default",
]
