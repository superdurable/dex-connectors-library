# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AddNoteInput, AddNoteOutput, CreateTicketInput, CreateTicketOutput, GetTicketInput, SearchTicketsInput, SearchTicketsOutput, Ticket, TicketConversation, TicketDetails, TicketRequester, TicketRequesterInput, UpdateTicketInput, UpdateTicketOutput
from .definitions import CONNECTOR_ID, ADDED, ADD_NOTE_DEFINITION, CREATED, CREATE_TICKET_DEFINITION, DEFECT, FOUND, GET_TICKET_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, SEARCHED, SEARCH_TICKETS_DEFINITION, UNCERTAIN, UPDATED, UPDATE_TICKET_DEFINITION

def search_tickets_searched(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_tickets_searched, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def search_tickets_provider_rejected(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_tickets_provider_rejected, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def search_tickets_invalid_response(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_tickets_invalid_response, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def search_tickets_defect(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_tickets_defect, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def search_tickets_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchTicketsOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_tickets_retry, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def search_tickets_customer_issue_pages() -> Case[SearchTicketsOutput]:
    return Case.paged(SEARCHED, (SearchTicketsOutput(tickets=(), total_matched=61, next_page=2), SearchTicketsOutput(tickets=(Ticket(id=1201, subject='Double charge on order 88213', status=3, priority=1, requester_id=6007738334, created_at=datetime.fromisoformat("2026-01-26T14:02:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-01-28T08:45:00Z".replace('Z', '+00:00'))),), total_matched=61, next_page=3), SearchTicketsOutput(tickets=(), total_matched=61, next_page=0)))

setattr(search_tickets_customer_issue_pages, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def search_tickets_search_rejected() -> Case[SearchTicketsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchTicketsOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "searchTickets", "Freshdesk rejected the ticket status filter."))

setattr(search_tickets_search_rejected, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def search_tickets_invalid_search_page() -> Case[SearchTicketsOutput]:
    return Case.branch_result(INVALID_RESPONSE, SearchTicketsOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "searchTickets", "Freshdesk returned an invalid search page."))

setattr(search_tickets_invalid_search_page, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def get_ticket_found(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_ticket_found, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def get_ticket_not_found(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_ticket_not_found, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def get_ticket_provider_rejected(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_ticket_provider_rejected, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def get_ticket_invalid_response(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_ticket_invalid_response, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def get_ticket_defect(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_ticket_defect, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def get_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[TicketDetails]:
    return Case.retry(failure, after_seconds)

setattr(get_ticket_retry, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def get_ticket_customer_issue_ticket() -> Case[TicketDetails]:
    return Case.branch_result(FOUND, TicketDetails(ticket=Ticket(id=1201, subject='Double charge on order 88213', description='I was charged twice.', status=3, priority=1, requester_id=6007738334, tags=('billing-double-charge',), created_at=datetime.fromisoformat("2026-01-26T14:02:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-01-28T08:45:00Z".replace('Z', '+00:00'))), requester=TicketRequester(id=6007738334, name='Jane Smith', email='jane@acme.example.com'), conversations=()))

setattr(get_ticket_customer_issue_ticket, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def get_ticket_ticket_missing() -> Case[TicketDetails]:
    return Case.branch_result(NOT_FOUND, TicketDetails(ticket=Ticket(status=0, priority=0)), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getTicket", "Freshdesk found no such ticket."))

setattr(get_ticket_ticket_missing, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def get_ticket_ticket_read_rejected() -> Case[TicketDetails]:
    return Case.branch_result(PROVIDER_REJECTED, TicketDetails(ticket=Ticket(status=0, priority=0)), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getTicket", "Freshdesk rejected the ticket read."))

setattr(get_ticket_ticket_read_rejected, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def get_ticket_invalid_ticket_read() -> Case[TicketDetails]:
    return Case.branch_result(INVALID_RESPONSE, TicketDetails(ticket=Ticket(status=0, priority=0)), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getTicket", "Freshdesk returned an invalid conversation page."))

setattr(get_ticket_invalid_ticket_read, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def create_ticket_created(value: CreateTicketOutput, failure: Failure | None = None) -> Case[CreateTicketOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_ticket_created, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def create_ticket_provider_rejected(value: CreateTicketOutput, failure: Failure | None = None) -> Case[CreateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_ticket_provider_rejected, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def create_ticket_uncertain(value: CreateTicketOutput, failure: Failure) -> Case[CreateTicketOutput]:
    return Case.uncertain(value, failure)

setattr(create_ticket_uncertain, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def create_ticket_defect(value: CreateTicketOutput, failure: Failure | None = None) -> Case[CreateTicketOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_ticket_defect, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def create_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateTicketOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_ticket_retry, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def create_ticket_created_customer_issue() -> Case[CreateTicketOutput]:
    return Case.branch_result(CREATED, CreateTicketOutput(ticket=Ticket(id=1202, subject='Double charge on order 88213', description='I was charged twice for order 88213.', status=2, priority=3, requester_id=6007738334, tags=('billing-double-charge',), created_at=datetime.fromisoformat("2026-01-29T10:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-01-29T10:00:00Z".replace('Z', '+00:00')))))

setattr(create_ticket_created_customer_issue, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def create_ticket_ticket_create_rejected() -> Case[CreateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateTicketOutput(ticket=Ticket(status=0, priority=0)), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "createTicket", "Freshdesk rejected the requested group."))

setattr(create_ticket_ticket_create_rejected, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def update_ticket_updated(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_ticket_updated, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def update_ticket_not_found(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_ticket_not_found, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def update_ticket_provider_rejected(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_ticket_provider_rejected, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def update_ticket_invalid_response(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_ticket_invalid_response, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def update_ticket_defect(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_ticket_defect, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def update_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateTicketOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_ticket_retry, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def update_ticket_reprioritized_customer_issue() -> Case[UpdateTicketOutput]:
    return Case.branch_result(UPDATED, UpdateTicketOutput(ticket=Ticket(id=1201, subject='Double charge on order 88213', description='I was charged twice.', status=2, priority=3, requester_id=6007738334, tags=('billing-double-charge', 'dex-repeat-contact'), created_at=datetime.fromisoformat("2026-01-26T14:02:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-01-29T10:01:00Z".replace('Z', '+00:00')))))

setattr(update_ticket_reprioritized_customer_issue, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def update_ticket_missing() -> Case[UpdateTicketOutput]:
    return Case.branch_result(NOT_FOUND, UpdateTicketOutput(ticket=Ticket(status=0, priority=0)), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateTicket", "Freshdesk found no such ticket."))

setattr(update_ticket_missing, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def update_ticket_update_rejected() -> Case[UpdateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateTicketOutput(ticket=Ticket(status=0, priority=0)), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "updateTicket", "Freshdesk rejected the ticket update."))

setattr(update_ticket_update_rejected, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def update_ticket_invalid_updated_ticket() -> Case[UpdateTicketOutput]:
    return Case.branch_result(INVALID_RESPONSE, UpdateTicketOutput(ticket=Ticket(status=0, priority=0)), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "updateTicket", "Freshdesk returned an invalid updated ticket."))

setattr(update_ticket_invalid_updated_ticket, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def add_note_added(value: AddNoteOutput, failure: Failure | None = None) -> Case[AddNoteOutput]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_note_added, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def add_note_not_found(value: AddNoteOutput, failure: Failure | None = None) -> Case[AddNoteOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_note_not_found, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def add_note_provider_rejected(value: AddNoteOutput, failure: Failure | None = None) -> Case[AddNoteOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_note_provider_rejected, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def add_note_uncertain(value: AddNoteOutput, failure: Failure) -> Case[AddNoteOutput]:
    return Case.uncertain(value, failure)

setattr(add_note_uncertain, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def add_note_defect(value: AddNoteOutput, failure: Failure | None = None) -> Case[AddNoteOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_note_defect, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def add_note_retry(failure: Failure, after_seconds: float = 0) -> Case[AddNoteOutput]:
    return Case.retry(failure, after_seconds)

setattr(add_note_retry, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def add_note_customer_issue_private_note() -> Case[AddNoteOutput]:
    return Case.branch_result(ADDED, AddNoteOutput(conversation=TicketConversation(id=7001, ticket_id=1201, user_id=6001263404, source=2, is_private=True, body='Dex triage: the customer contacted us again about billing-double-charge. Priority set to High (3).\n\nCustomer message:\nI was charged twice for order 88213.', created_at=datetime.fromisoformat("2026-01-29T10:02:00Z".replace('Z', '+00:00')))))

setattr(add_note_customer_issue_private_note, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def add_note_note_ticket_missing() -> Case[AddNoteOutput]:
    return Case.branch_result(NOT_FOUND, AddNoteOutput(conversation=TicketConversation(source=0)), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addNote", "Freshdesk found no such ticket for the note."))

setattr(add_note_note_ticket_missing, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

def add_note_note_rejected() -> Case[AddNoteOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AddNoteOutput(conversation=TicketConversation(source=0)), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "addNote", "Freshdesk rejected the note."))

setattr(add_note_note_rejected, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")


class Mock:

    def __init__(self, connection_name: str = "freshdesk") -> None:
        self.search_tickets_script: Query[SearchTicketsInput, SearchTicketsOutput] = Query(SEARCH_TICKETS_DEFINITION)
        self.search_tickets_script.default(search_tickets_customer_issue_pages())
        self.get_ticket_script: Query[GetTicketInput, TicketDetails] = Query(GET_TICKET_DEFINITION)
        self.get_ticket_script.default(get_ticket_customer_issue_ticket())
        self.create_ticket_script: Mutation[CreateTicketInput, CreateTicketOutput] = Mutation(CREATE_TICKET_DEFINITION)
        self.create_ticket_script.default(create_ticket_created_customer_issue())
        self.update_ticket_script: Mutation[UpdateTicketInput, UpdateTicketOutput] = Mutation(UPDATE_TICKET_DEFINITION)
        self.update_ticket_script.default(update_ticket_reprioritized_customer_issue())
        self.add_note_script: Mutation[AddNoteInput, AddNoteOutput] = Mutation(ADD_NOTE_DEFINITION)
        self.add_note_script.default(add_note_customer_issue_private_note())
        self._connection = new_connection_with_operations(
            search_tickets=self.search_tickets_script,
            get_ticket=self.get_ticket_script,
            create_ticket=self.create_ticket_script,
            update_ticket=self.update_ticket_script,
            add_note=self.add_note_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.freshworks_freshdesk.freshdeskmock")

__all__ = [
    "Mock",
    "search_tickets_searched",
    "search_tickets_provider_rejected",
    "search_tickets_invalid_response",
    "search_tickets_defect",
    "search_tickets_retry",
    "search_tickets_customer_issue_pages",
    "search_tickets_search_rejected",
    "search_tickets_invalid_search_page",
    "get_ticket_found",
    "get_ticket_not_found",
    "get_ticket_provider_rejected",
    "get_ticket_invalid_response",
    "get_ticket_defect",
    "get_ticket_retry",
    "get_ticket_customer_issue_ticket",
    "get_ticket_ticket_missing",
    "get_ticket_ticket_read_rejected",
    "get_ticket_invalid_ticket_read",
    "create_ticket_created",
    "create_ticket_provider_rejected",
    "create_ticket_uncertain",
    "create_ticket_defect",
    "create_ticket_retry",
    "create_ticket_created_customer_issue",
    "create_ticket_ticket_create_rejected",
    "update_ticket_updated",
    "update_ticket_not_found",
    "update_ticket_provider_rejected",
    "update_ticket_invalid_response",
    "update_ticket_defect",
    "update_ticket_retry",
    "update_ticket_reprioritized_customer_issue",
    "update_ticket_missing",
    "update_ticket_update_rejected",
    "update_ticket_invalid_updated_ticket",
    "add_note_added",
    "add_note_not_found",
    "add_note_provider_rejected",
    "add_note_uncertain",
    "add_note_defect",
    "add_note_retry",
    "add_note_customer_issue_private_note",
    "add_note_note_ticket_missing",
    "add_note_note_rejected",
]
