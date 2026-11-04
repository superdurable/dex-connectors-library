# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import (
    CreateTicketInput,
    CreateTicketOutput,
    GetTicketInput,
    SearchTicketsInput,
    SearchTicketsOutput,
    Ticket,
    TicketComment,
    TicketCommentInput,
    TicketDetails,
    TicketRequesterInput,
    TicketUser,
    UpdateTicketInput,
    UpdateTicketOutput,
)
from .definitions import (
    CONNECTOR_ID,
    CONFLICT,
    CREATED,
    CREATE_TICKET_DEFINITION,
    DEFECT,
    FOUND,
    GET_TICKET_DEFINITION,
    INVALID_RESPONSE,
    NOT_FOUND,
    PROVIDER_REJECTED,
    SEARCHED,
    SEARCH_TICKETS_DEFINITION,
    UPDATED,
    UPDATE_TICKET_DEFINITION,
)


def search_tickets_searched(
    value: SearchTicketsOutput, failure: Failure | None = None
) -> Case[SearchTicketsOutput]:
    return Case.branch_result(SEARCHED, value, failure)


setattr(search_tickets_searched, "__module__", "dex_connectors.zendesk_support.supportmock")


def search_tickets_provider_rejected(
    value: SearchTicketsOutput, failure: Failure | None = None
) -> Case[SearchTicketsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)


setattr(search_tickets_provider_rejected, "__module__", "dex_connectors.zendesk_support.supportmock")


def search_tickets_invalid_response(
    value: SearchTicketsOutput, failure: Failure | None = None
) -> Case[SearchTicketsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)


setattr(search_tickets_invalid_response, "__module__", "dex_connectors.zendesk_support.supportmock")


def search_tickets_defect(
    value: SearchTicketsOutput, failure: Failure | None = None
) -> Case[SearchTicketsOutput]:
    return Case.branch_result(DEFECT, value, failure)


setattr(search_tickets_defect, "__module__", "dex_connectors.zendesk_support.supportmock")


def search_tickets_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchTicketsOutput]:
    return Case.retry(failure, after_seconds)


setattr(search_tickets_retry, "__module__", "dex_connectors.zendesk_support.supportmock")


def search_tickets_customer_issue_tickets() -> Case[SearchTicketsOutput]:
    return Case.paged(
        SEARCHED,
        (
            SearchTicketsOutput(
                tickets=(
                    Ticket(
                        id=35435,
                        agent_url="https://acme.zendesk.com/agent/tickets/35435",
                        subject="Double charge on order 88213",
                        status="pending",
                        requester_id=7701,
                        tags=("billing-double-charge",),
                        created_at=datetime.fromisoformat("2026-10-20T08:00:00Z".replace("Z", "+00:00")),
                        updated_at=datetime.fromisoformat("2026-10-21T08:00:00Z".replace("Z", "+00:00")),
                    ),
                    Ticket(
                        id=35436,
                        agent_url="https://acme.zendesk.com/agent/tickets/35436",
                        subject="Double charge on order 88213",
                        status="pending",
                        requester_id=7701,
                        tags=("billing-double-charge",),
                        created_at=datetime.fromisoformat("2026-10-21T08:00:00Z".replace("Z", "+00:00")),
                        updated_at=datetime.fromisoformat("2026-10-22T08:00:00Z".replace("Z", "+00:00")),
                    ),
                ),
                next_cursor="cursor-1",
            ),
            SearchTicketsOutput(
                tickets=(
                    Ticket(
                        id=35438,
                        agent_url="https://acme.zendesk.com/agent/tickets/35438",
                        subject="Double charge on order 88213",
                        status="solved",
                        requester_id=7701,
                        tags=("billing-double-charge",),
                        created_at=datetime.fromisoformat("2026-10-22T09:00:00Z".replace("Z", "+00:00")),
                        updated_at=datetime.fromisoformat("2026-10-23T09:00:00Z".replace("Z", "+00:00")),
                    ),
                ),
                next_cursor="cursor-2",
            ),
            SearchTicketsOutput(tickets=(), next_cursor=""),
        ),
    )


setattr(search_tickets_customer_issue_tickets, "__module__", "dex_connectors.zendesk_support.supportmock")


def search_tickets_access_denied() -> Case[SearchTicketsOutput]:
    return Case.branch_result(
        PROVIDER_REJECTED,
        SearchTicketsOutput(),
        Failure(
            FailureKind.AUTHORIZATION,
            CONNECTOR_ID,
            "searchTickets",
            "Zendesk rejected the request with HTTP 403.",
        ),
    )


setattr(search_tickets_access_denied, "__module__", "dex_connectors.zendesk_support.supportmock")


def get_ticket_found(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(FOUND, value, failure)


setattr(get_ticket_found, "__module__", "dex_connectors.zendesk_support.supportmock")


def get_ticket_not_found(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(NOT_FOUND, value, failure)


setattr(get_ticket_not_found, "__module__", "dex_connectors.zendesk_support.supportmock")


def get_ticket_provider_rejected(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)


setattr(get_ticket_provider_rejected, "__module__", "dex_connectors.zendesk_support.supportmock")


def get_ticket_invalid_response(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)


setattr(get_ticket_invalid_response, "__module__", "dex_connectors.zendesk_support.supportmock")


def get_ticket_defect(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(DEFECT, value, failure)


setattr(get_ticket_defect, "__module__", "dex_connectors.zendesk_support.supportmock")


def get_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[TicketDetails]:
    return Case.retry(failure, after_seconds)


setattr(get_ticket_retry, "__module__", "dex_connectors.zendesk_support.supportmock")


def get_ticket_customer_issue_ticket() -> Case[TicketDetails]:
    return Case.branch_result(
        FOUND,
        TicketDetails(
            ticket=Ticket(
                id=35436,
                agent_url="https://acme.zendesk.com/agent/tickets/35436",
                subject="Double charge on order 88213",
                status="pending",
                requester_id=7701,
                tags=("billing-double-charge",),
                created_at=datetime.fromisoformat("2026-10-21T08:00:00Z".replace("Z", "+00:00")),
                updated_at=datetime.fromisoformat("2026-10-22T08:00:00Z".replace("Z", "+00:00")),
            ),
            requester=TicketUser(id=7701, name="Jane Smith", email="jane@acme.example.com", role="end-user"),
            latest_comments=(
                TicketComment(
                    id=88001,
                    author_id=7701,
                    is_public=True,
                    body="I was charged twice for order 88213.",
                    created_at=datetime.fromisoformat("2026-10-21T08:00:00Z".replace("Z", "+00:00")),
                ),
            ),
        ),
    )


setattr(get_ticket_customer_issue_ticket, "__module__", "dex_connectors.zendesk_support.supportmock")


def get_ticket_missing_ticket() -> Case[TicketDetails]:
    return Case.branch_result(
        NOT_FOUND,
        TicketDetails(ticket=Ticket(status="")),
        Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getTicket", "Zendesk found no such resource."),
    )


setattr(get_ticket_missing_ticket, "__module__", "dex_connectors.zendesk_support.supportmock")


def get_ticket_access_denied() -> Case[TicketDetails]:
    return Case.branch_result(
        PROVIDER_REJECTED,
        TicketDetails(ticket=Ticket(status="")),
        Failure(
            FailureKind.AUTHORIZATION,
            CONNECTOR_ID,
            "getTicket",
            "Zendesk rejected the request with HTTP 403.",
        ),
    )


setattr(get_ticket_access_denied, "__module__", "dex_connectors.zendesk_support.supportmock")


def create_ticket_created(
    value: CreateTicketOutput, failure: Failure | None = None
) -> Case[CreateTicketOutput]:
    return Case.branch_result(CREATED, value, failure)


setattr(create_ticket_created, "__module__", "dex_connectors.zendesk_support.supportmock")


def create_ticket_provider_rejected(
    value: CreateTicketOutput, failure: Failure | None = None
) -> Case[CreateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)


setattr(create_ticket_provider_rejected, "__module__", "dex_connectors.zendesk_support.supportmock")


def create_ticket_invalid_response(
    value: CreateTicketOutput, failure: Failure | None = None
) -> Case[CreateTicketOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)


setattr(create_ticket_invalid_response, "__module__", "dex_connectors.zendesk_support.supportmock")


def create_ticket_defect(
    value: CreateTicketOutput, failure: Failure | None = None
) -> Case[CreateTicketOutput]:
    return Case.branch_result(DEFECT, value, failure)


setattr(create_ticket_defect, "__module__", "dex_connectors.zendesk_support.supportmock")


def create_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateTicketOutput]:
    return Case.retry(failure, after_seconds)


setattr(create_ticket_retry, "__module__", "dex_connectors.zendesk_support.supportmock")


def create_ticket_opened_customer_issue() -> Case[CreateTicketOutput]:
    return Case.branch_result(
        CREATED,
        CreateTicketOutput(
            ticket=Ticket(
                id=35437,
                agent_url="https://acme.zendesk.com/agent/tickets/35437",
                subject="Double charge on order 88213",
                description="I was charged twice for order 88213.",
                status="open",
                priority="normal",
                requester_id=7701,
                tags=("billing-double-charge",),
                created_at=datetime.fromisoformat("2026-10-26T12:00:00Z".replace("Z", "+00:00")),
                updated_at=datetime.fromisoformat("2026-10-26T12:00:00Z".replace("Z", "+00:00")),
            )
        ),
    )


setattr(create_ticket_opened_customer_issue, "__module__", "dex_connectors.zendesk_support.supportmock")


def create_ticket_access_denied() -> Case[CreateTicketOutput]:
    return Case.branch_result(
        PROVIDER_REJECTED,
        CreateTicketOutput(ticket=Ticket(status="")),
        Failure(
            FailureKind.AUTHORIZATION,
            CONNECTOR_ID,
            "createTicket",
            "Zendesk rejected the request with HTTP 403.",
        ),
    )


setattr(create_ticket_access_denied, "__module__", "dex_connectors.zendesk_support.supportmock")


def update_ticket_updated(
    value: UpdateTicketOutput, failure: Failure | None = None
) -> Case[UpdateTicketOutput]:
    return Case.branch_result(UPDATED, value, failure)


setattr(update_ticket_updated, "__module__", "dex_connectors.zendesk_support.supportmock")


def update_ticket_not_found(
    value: UpdateTicketOutput, failure: Failure | None = None
) -> Case[UpdateTicketOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)


setattr(update_ticket_not_found, "__module__", "dex_connectors.zendesk_support.supportmock")


def update_ticket_conflict(
    value: UpdateTicketOutput, failure: Failure | None = None
) -> Case[UpdateTicketOutput]:
    return Case.branch_result(CONFLICT, value, failure)


setattr(update_ticket_conflict, "__module__", "dex_connectors.zendesk_support.supportmock")


def update_ticket_provider_rejected(
    value: UpdateTicketOutput, failure: Failure | None = None
) -> Case[UpdateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)


setattr(update_ticket_provider_rejected, "__module__", "dex_connectors.zendesk_support.supportmock")


def update_ticket_invalid_response(
    value: UpdateTicketOutput, failure: Failure | None = None
) -> Case[UpdateTicketOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)


setattr(update_ticket_invalid_response, "__module__", "dex_connectors.zendesk_support.supportmock")


def update_ticket_defect(
    value: UpdateTicketOutput, failure: Failure | None = None
) -> Case[UpdateTicketOutput]:
    return Case.branch_result(DEFECT, value, failure)


setattr(update_ticket_defect, "__module__", "dex_connectors.zendesk_support.supportmock")


def update_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateTicketOutput]:
    return Case.retry(failure, after_seconds)


setattr(update_ticket_retry, "__module__", "dex_connectors.zendesk_support.supportmock")


def update_ticket_followed_up_customer_issue() -> Case[UpdateTicketOutput]:
    return Case.branch_result(
        UPDATED,
        UpdateTicketOutput(
            ticket=Ticket(
                id=35436,
                agent_url="https://acme.zendesk.com/agent/tickets/35436",
                subject="Double charge on order 88213",
                status="open",
                requester_id=7701,
                tags=("billing-double-charge", "dex-repeat-contact"),
                created_at=datetime.fromisoformat("2026-10-21T08:00:00Z".replace("Z", "+00:00")),
                updated_at=datetime.fromisoformat("2026-10-26T12:00:00Z".replace("Z", "+00:00")),
            ),
            was_already_applied=False,
        ),
    )


setattr(update_ticket_followed_up_customer_issue, "__module__", "dex_connectors.zendesk_support.supportmock")


def update_ticket_missing_ticket() -> Case[UpdateTicketOutput]:
    return Case.branch_result(
        NOT_FOUND,
        UpdateTicketOutput(ticket=Ticket(status="")),
        Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateTicket", "Zendesk found no such resource."),
    )


setattr(update_ticket_missing_ticket, "__module__", "dex_connectors.zendesk_support.supportmock")


def update_ticket_ticket_changed() -> Case[UpdateTicketOutput]:
    return Case.branch_result(
        CONFLICT,
        UpdateTicketOutput(
            ticket=Ticket(
                id=35436,
                agent_url="https://acme.zendesk.com/agent/tickets/35436",
                subject="Double charge on order 88213",
                status="pending",
                requester_id=7701,
                tags=("billing-double-charge",),
                created_at=datetime.fromisoformat("2026-10-21T08:00:00Z".replace("Z", "+00:00")),
                updated_at=datetime.fromisoformat("2026-10-22T08:00:00Z".replace("Z", "+00:00")),
            )
        ),
        Failure(
            FailureKind.CONFLICT, CONNECTOR_ID, "updateTicket", "The ticket changed after expectedUpdatedAt."
        ),
    )


setattr(update_ticket_ticket_changed, "__module__", "dex_connectors.zendesk_support.supportmock")


def update_ticket_access_denied() -> Case[UpdateTicketOutput]:
    return Case.branch_result(
        PROVIDER_REJECTED,
        UpdateTicketOutput(ticket=Ticket(status="")),
        Failure(
            FailureKind.AUTHORIZATION,
            CONNECTOR_ID,
            "updateTicket",
            "Zendesk rejected the request with HTTP 403.",
        ),
    )


setattr(update_ticket_access_denied, "__module__", "dex_connectors.zendesk_support.supportmock")


class Mock:
    def __init__(self, connection_name: str = "zendesk-support") -> None:
        self.search_tickets_script: Query[SearchTicketsInput, SearchTicketsOutput] = Query(
            SEARCH_TICKETS_DEFINITION
        )
        self.search_tickets_script.default(search_tickets_customer_issue_tickets())
        self.get_ticket_script: Query[GetTicketInput, TicketDetails] = Query(GET_TICKET_DEFINITION)
        self.get_ticket_script.default(get_ticket_customer_issue_ticket())
        self.create_ticket_script: Mutation[CreateTicketInput, CreateTicketOutput] = Mutation(
            CREATE_TICKET_DEFINITION
        )
        self.create_ticket_script.default(create_ticket_opened_customer_issue())
        self.update_ticket_script: Mutation[UpdateTicketInput, UpdateTicketOutput] = Mutation(
            UPDATE_TICKET_DEFINITION
        )
        self.update_ticket_script.default(update_ticket_followed_up_customer_issue())
        self._connection = new_connection_with_operations(
            search_tickets=self.search_tickets_script,
            get_ticket=self.get_ticket_script,
            create_ticket=self.create_ticket_script,
            update_ticket=self.update_ticket_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection


setattr(Mock, "__module__", "dex_connectors.zendesk_support.supportmock")

__all__ = [
    "Mock",
    "search_tickets_searched",
    "search_tickets_provider_rejected",
    "search_tickets_invalid_response",
    "search_tickets_defect",
    "search_tickets_retry",
    "search_tickets_customer_issue_tickets",
    "search_tickets_access_denied",
    "get_ticket_found",
    "get_ticket_not_found",
    "get_ticket_provider_rejected",
    "get_ticket_invalid_response",
    "get_ticket_defect",
    "get_ticket_retry",
    "get_ticket_customer_issue_ticket",
    "get_ticket_missing_ticket",
    "get_ticket_access_denied",
    "create_ticket_created",
    "create_ticket_provider_rejected",
    "create_ticket_invalid_response",
    "create_ticket_defect",
    "create_ticket_retry",
    "create_ticket_opened_customer_issue",
    "create_ticket_access_denied",
    "update_ticket_updated",
    "update_ticket_not_found",
    "update_ticket_conflict",
    "update_ticket_provider_rejected",
    "update_ticket_invalid_response",
    "update_ticket_defect",
    "update_ticket_retry",
    "update_ticket_followed_up_customer_issue",
    "update_ticket_missing_ticket",
    "update_ticket_ticket_changed",
    "update_ticket_access_denied",
]
