# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetTicketInput, GetTicketOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_TICKET_DEFINITION, INVALID_RESPONSE, NO_DATA, PROVIDER_REJECTED

def get_ticket_found(value: GetTicketOutput, failure: Failure | None = None) -> Case[GetTicketOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_ticket_found, '__module__', "dex_connectors.kf5.kf5mock")

def get_ticket_no_data(value: GetTicketOutput, failure: Failure | None = None) -> Case[GetTicketOutput]:
    return Case.branch_result(NO_DATA, value, failure)

setattr(get_ticket_no_data, '__module__', "dex_connectors.kf5.kf5mock")

def get_ticket_provider_rejected(value: GetTicketOutput, failure: Failure | None = None) -> Case[GetTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_ticket_provider_rejected, '__module__', "dex_connectors.kf5.kf5mock")

def get_ticket_invalid_response(value: GetTicketOutput, failure: Failure | None = None) -> Case[GetTicketOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_ticket_invalid_response, '__module__', "dex_connectors.kf5.kf5mock")

def get_ticket_defect(value: GetTicketOutput, failure: Failure | None = None) -> Case[GetTicketOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_ticket_defect, '__module__', "dex_connectors.kf5.kf5mock")

def get_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[GetTicketOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_ticket_retry, '__module__', "dex_connectors.kf5.kf5mock")

def get_ticket_open_ticket() -> Case[GetTicketOutput]:
    return Case.branch_result(FOUND, GetTicketOutput(ticket_id=123, status='open', priority='normal', type='question', created_at='2026-11-13 10:00:00', updated_at='2026-11-13 11:00:00'))

setattr(get_ticket_open_ticket, '__module__', "dex_connectors.kf5.kf5mock")

def get_ticket_no_ticket() -> Case[GetTicketOutput]:
    return Case.branch_result(NO_DATA, GetTicketOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getTicket", "KF5 did not find the ticket."))

setattr(get_ticket_no_ticket, '__module__', "dex_connectors.kf5.kf5mock")


class Mock:

    def __init__(self, connection_name: str = "kf5") -> None:
        self.get_ticket_script: Query[GetTicketInput, GetTicketOutput] = Query(GET_TICKET_DEFINITION)
        self.get_ticket_script.default(get_ticket_open_ticket())
        self._connection = new_connection_with_operations(
            get_ticket=self.get_ticket_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.kf5.kf5mock")

__all__ = [
    "Mock",
    "get_ticket_found",
    "get_ticket_no_data",
    "get_ticket_provider_rejected",
    "get_ticket_invalid_response",
    "get_ticket_defect",
    "get_ticket_retry",
    "get_ticket_open_ticket",
    "get_ticket_no_ticket",
]
