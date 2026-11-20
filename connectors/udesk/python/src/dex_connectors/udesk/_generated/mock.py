# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListTicketStatusesInput, ListTicketStatusesOutput, TicketStatus
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_TICKET_STATUSES_DEFINITION, PROVIDER_REJECTED

def list_ticket_statuses_listed(value: ListTicketStatusesOutput, failure: Failure | None = None) -> Case[ListTicketStatusesOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_ticket_statuses_listed, '__module__', "dex_connectors.udesk.udeskmock")

def list_ticket_statuses_provider_rejected(value: ListTicketStatusesOutput, failure: Failure | None = None) -> Case[ListTicketStatusesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_ticket_statuses_provider_rejected, '__module__', "dex_connectors.udesk.udeskmock")

def list_ticket_statuses_invalid_response(value: ListTicketStatusesOutput, failure: Failure | None = None) -> Case[ListTicketStatusesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_ticket_statuses_invalid_response, '__module__', "dex_connectors.udesk.udeskmock")

def list_ticket_statuses_defect(value: ListTicketStatusesOutput, failure: Failure | None = None) -> Case[ListTicketStatusesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_ticket_statuses_defect, '__module__', "dex_connectors.udesk.udeskmock")

def list_ticket_statuses_retry(failure: Failure, after_seconds: float = 0) -> Case[ListTicketStatusesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_ticket_statuses_retry, '__module__', "dex_connectors.udesk.udeskmock")

def list_ticket_statuses_common_statuses() -> Case[ListTicketStatusesOutput]:
    return Case.branch_result(LISTED, ListTicketStatusesOutput(statuses=(TicketStatus(status_id=1, name='开启', english_name='open', enabled=True, color='#f30000'), TicketStatus(status_id=2, name='已解决', english_name='resolved', enabled=True, color='#00b38a'))))

setattr(list_ticket_statuses_common_statuses, '__module__', "dex_connectors.udesk.udeskmock")


class Mock:

    def __init__(self, connection_name: str = "udesk") -> None:
        self.list_ticket_statuses_script: Query[ListTicketStatusesInput, ListTicketStatusesOutput] = Query(LIST_TICKET_STATUSES_DEFINITION)
        self.list_ticket_statuses_script.default(list_ticket_statuses_common_statuses())
        self._connection = new_connection_with_operations(
            list_ticket_statuses=self.list_ticket_statuses_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.udesk.udeskmock")

__all__ = [
    "Mock",
    "list_ticket_statuses_listed",
    "list_ticket_statuses_provider_rejected",
    "list_ticket_statuses_invalid_response",
    "list_ticket_statuses_defect",
    "list_ticket_statuses_retry",
    "list_ticket_statuses_common_statuses",
]
