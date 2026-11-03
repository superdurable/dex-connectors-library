# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AddNoteInput, AddNoteOutput, CreateTicketInput, CreateTicketOutput, Customer, CustomerMatches, FindCustomerByEmailInput, GetTicketInput, SearchTicketsInput, SearchTicketsOutput, Ticket, TicketDetails, TicketMessage, TicketRequesterInput, UpdateTicketInput, UpdateTicketOutput
from .definitions import CONNECTOR_ID, ADDED, ADD_NOTE_DEFINITION, CREATED, CREATE_TICKET_DEFINITION, DEFECT, FIND_CUSTOMER_BY_EMAIL_DEFINITION, FOUND, GET_TICKET_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, SEARCHED, SEARCH_TICKETS_DEFINITION, UNCERTAIN, UPDATED, UPDATE_TICKET_DEFINITION

def search_tickets_searched(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_tickets_searched, '__module__', "dex_connectors.gorgias.gorgiasmock")

def search_tickets_provider_rejected(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_tickets_provider_rejected, '__module__', "dex_connectors.gorgias.gorgiasmock")

def search_tickets_invalid_response(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_tickets_invalid_response, '__module__', "dex_connectors.gorgias.gorgiasmock")

def search_tickets_defect(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_tickets_defect, '__module__', "dex_connectors.gorgias.gorgiasmock")

def search_tickets_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchTicketsOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_tickets_retry, '__module__', "dex_connectors.gorgias.gorgiasmock")

def search_tickets_open_billing_ticket() -> Case[SearchTicketsOutput]:
    return Case.paged(SEARCHED, (SearchTicketsOutput(tickets=(Ticket(id=7001, subject='Duplicate charge', status='open', priority='normal', channel='email', requester_id=501, requester_email='ada@example.com', tags=('billing-double-charge',), created_at=datetime.fromisoformat("2026-10-20T09:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-25T09:00:00Z".replace('Z', '+00:00'))),), next_cursor='billing-page-2', listed_count=100), SearchTicketsOutput(tickets=(Ticket(id=7001, subject='Duplicate charge', status='open', priority='normal', channel='email', requester_id=501, requester_email='ada@example.com', tags=('billing-double-charge',), created_at=datetime.fromisoformat("2026-10-20T09:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-25T09:00:00Z".replace('Z', '+00:00'))), Ticket(id=6999, subject='Earlier duplicate charge', status='open', priority='normal', channel='email', requester_id=501, requester_email='ada@example.com', tags=('billing-double-charge',), created_at=datetime.fromisoformat("2026-10-15T09:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-20T09:00:00Z".replace('Z', '+00:00')))), next_cursor='billing-page-3', listed_count=100), SearchTicketsOutput(tickets=(), listed_count=0)))

setattr(search_tickets_open_billing_ticket, '__module__', "dex_connectors.gorgias.gorgiasmock")

def search_tickets_provider_rejected_failure() -> Case[SearchTicketsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchTicketsOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "searchTickets", "Gorgias denied the requested operation."))

setattr(search_tickets_provider_rejected_failure, '__module__', "dex_connectors.gorgias.gorgiasmock")

def get_ticket_found(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_ticket_found, '__module__', "dex_connectors.gorgias.gorgiasmock")

def get_ticket_not_found(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_ticket_not_found, '__module__', "dex_connectors.gorgias.gorgiasmock")

def get_ticket_provider_rejected(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_ticket_provider_rejected, '__module__', "dex_connectors.gorgias.gorgiasmock")

def get_ticket_invalid_response(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_ticket_invalid_response, '__module__', "dex_connectors.gorgias.gorgiasmock")

def get_ticket_defect(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_ticket_defect, '__module__', "dex_connectors.gorgias.gorgiasmock")

def get_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[TicketDetails]:
    return Case.retry(failure, after_seconds)

setattr(get_ticket_retry, '__module__', "dex_connectors.gorgias.gorgiasmock")

def get_ticket_billing_ticket_details() -> Case[TicketDetails]:
    return Case.branch_result(FOUND, TicketDetails(ticket=Ticket(id=7001, subject='Duplicate charge', status='open', priority='normal', channel='email', requester_id=501, requester_email='ada@example.com', tags=('billing-double-charge',), created_at=datetime.fromisoformat("2026-10-20T09:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-25T09:00:00Z".replace('Z', '+00:00'))), requester=Customer(id=501, email='ada@example.com', name='Ada Lovelace'), latest_messages=(TicketMessage(id=8001, ticket_id=7001, channel='email', via='email', is_public=True, is_from_agent=False, sender_id=501, subject='Duplicate charge', body='I was charged twice.', created_at=datetime.fromisoformat("2026-10-20T09:00:00Z".replace('Z', '+00:00'))),)))

setattr(get_ticket_billing_ticket_details, '__module__', "dex_connectors.gorgias.gorgiasmock")

def get_ticket_not_found_failure() -> Case[TicketDetails]:
    return Case.branch_result(NOT_FOUND, TicketDetails(ticket=Ticket(status='')), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getTicket", "The ticket does not exist or is not visible to the connection's user."))

setattr(get_ticket_not_found_failure, '__module__', "dex_connectors.gorgias.gorgiasmock")

def get_ticket_provider_rejected_failure() -> Case[TicketDetails]:
    return Case.branch_result(PROVIDER_REJECTED, TicketDetails(ticket=Ticket(status='')), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getTicket", "Gorgias denied the requested operation."))

setattr(get_ticket_provider_rejected_failure, '__module__', "dex_connectors.gorgias.gorgiasmock")

def find_customer_by_email_found(value: CustomerMatches, failure: Failure | None = None) -> Case[CustomerMatches]:
    return Case.branch_result(FOUND, value, failure)

setattr(find_customer_by_email_found, '__module__', "dex_connectors.gorgias.gorgiasmock")

def find_customer_by_email_not_found(value: CustomerMatches, failure: Failure | None = None) -> Case[CustomerMatches]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(find_customer_by_email_not_found, '__module__', "dex_connectors.gorgias.gorgiasmock")

def find_customer_by_email_provider_rejected(value: CustomerMatches, failure: Failure | None = None) -> Case[CustomerMatches]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(find_customer_by_email_provider_rejected, '__module__', "dex_connectors.gorgias.gorgiasmock")

def find_customer_by_email_invalid_response(value: CustomerMatches, failure: Failure | None = None) -> Case[CustomerMatches]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(find_customer_by_email_invalid_response, '__module__', "dex_connectors.gorgias.gorgiasmock")

def find_customer_by_email_defect(value: CustomerMatches, failure: Failure | None = None) -> Case[CustomerMatches]:
    return Case.branch_result(DEFECT, value, failure)

setattr(find_customer_by_email_defect, '__module__', "dex_connectors.gorgias.gorgiasmock")

def find_customer_by_email_retry(failure: Failure, after_seconds: float = 0) -> Case[CustomerMatches]:
    return Case.retry(failure, after_seconds)

setattr(find_customer_by_email_retry, '__module__', "dex_connectors.gorgias.gorgiasmock")

def find_customer_by_email_ada_customer() -> Case[CustomerMatches]:
    return Case.branch_result(FOUND, CustomerMatches(email='ada@example.com', customers=(Customer(id=501, email='ada@example.com', name='Ada Lovelace'),)))

setattr(find_customer_by_email_ada_customer, '__module__', "dex_connectors.gorgias.gorgiasmock")

def find_customer_by_email_customer_absent() -> Case[CustomerMatches]:
    return Case.branch_result(NOT_FOUND, CustomerMatches(email='nobody@example.com', customers=()))

setattr(find_customer_by_email_customer_absent, '__module__', "dex_connectors.gorgias.gorgiasmock")

def find_customer_by_email_provider_rejected_failure() -> Case[CustomerMatches]:
    return Case.branch_result(PROVIDER_REJECTED, CustomerMatches(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "findCustomerByEmail", "Gorgias denied the requested operation."))

setattr(find_customer_by_email_provider_rejected_failure, '__module__', "dex_connectors.gorgias.gorgiasmock")

def create_ticket_created(value: CreateTicketOutput, failure: Failure | None = None) -> Case[CreateTicketOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_ticket_created, '__module__', "dex_connectors.gorgias.gorgiasmock")

def create_ticket_provider_rejected(value: CreateTicketOutput, failure: Failure | None = None) -> Case[CreateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_ticket_provider_rejected, '__module__', "dex_connectors.gorgias.gorgiasmock")

def create_ticket_uncertain(value: CreateTicketOutput, failure: Failure) -> Case[CreateTicketOutput]:
    return Case.uncertain(value, failure)

setattr(create_ticket_uncertain, '__module__', "dex_connectors.gorgias.gorgiasmock")

def create_ticket_defect(value: CreateTicketOutput, failure: Failure | None = None) -> Case[CreateTicketOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_ticket_defect, '__module__', "dex_connectors.gorgias.gorgiasmock")

def create_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateTicketOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_ticket_retry, '__module__', "dex_connectors.gorgias.gorgiasmock")

def create_ticket_refund_ticket_created() -> Case[CreateTicketOutput]:
    return Case.branch_result(CREATED, CreateTicketOutput(ticket=Ticket(id=7002, subject='Refund question', status='open', priority='high', channel='email', requester_id=502, requester_email='nobody@example.com', tags=('billing-refund',), created_at=datetime.fromisoformat("2026-10-26T09:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-26T09:00:00Z".replace('Z', '+00:00'))), was_created_by_earlier_attempt=False))

setattr(create_ticket_refund_ticket_created, '__module__', "dex_connectors.gorgias.gorgiasmock")

def create_ticket_provider_rejected_failure() -> Case[CreateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateTicketOutput(ticket=Ticket(status='')), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "createTicket", "Gorgias denied the requested operation."))

setattr(create_ticket_provider_rejected_failure, '__module__', "dex_connectors.gorgias.gorgiasmock")

def update_ticket_updated(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_ticket_updated, '__module__', "dex_connectors.gorgias.gorgiasmock")

def update_ticket_not_found(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_ticket_not_found, '__module__', "dex_connectors.gorgias.gorgiasmock")

def update_ticket_provider_rejected(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_ticket_provider_rejected, '__module__', "dex_connectors.gorgias.gorgiasmock")

def update_ticket_invalid_response(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_ticket_invalid_response, '__module__', "dex_connectors.gorgias.gorgiasmock")

def update_ticket_defect(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_ticket_defect, '__module__', "dex_connectors.gorgias.gorgiasmock")

def update_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateTicketOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_ticket_retry, '__module__', "dex_connectors.gorgias.gorgiasmock")

def update_ticket_billing_ticket_prioritized() -> Case[UpdateTicketOutput]:
    return Case.branch_result(UPDATED, UpdateTicketOutput(ticket=Ticket(id=7001, subject='Duplicate charge', status='open', priority='high', channel='email', requester_id=501, requester_email='ada@example.com', tags=('billing-double-charge', 'dex-repeat-contact'), created_at=datetime.fromisoformat("2026-10-20T09:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-26T09:00:00Z".replace('Z', '+00:00'))), was_already_applied=False))

setattr(update_ticket_billing_ticket_prioritized, '__module__', "dex_connectors.gorgias.gorgiasmock")

def update_ticket_not_found_failure() -> Case[UpdateTicketOutput]:
    return Case.branch_result(NOT_FOUND, UpdateTicketOutput(ticket=Ticket(status='')), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateTicket", "The ticket does not exist or is not visible to the connection's user."))

setattr(update_ticket_not_found_failure, '__module__', "dex_connectors.gorgias.gorgiasmock")

def update_ticket_provider_rejected_failure() -> Case[UpdateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateTicketOutput(ticket=Ticket(status='')), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "updateTicket", "Gorgias denied the requested operation."))

setattr(update_ticket_provider_rejected_failure, '__module__', "dex_connectors.gorgias.gorgiasmock")

def add_note_added(value: AddNoteOutput, failure: Failure | None = None) -> Case[AddNoteOutput]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_note_added, '__module__', "dex_connectors.gorgias.gorgiasmock")

def add_note_not_found(value: AddNoteOutput, failure: Failure | None = None) -> Case[AddNoteOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_note_not_found, '__module__', "dex_connectors.gorgias.gorgiasmock")

def add_note_provider_rejected(value: AddNoteOutput, failure: Failure | None = None) -> Case[AddNoteOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_note_provider_rejected, '__module__', "dex_connectors.gorgias.gorgiasmock")

def add_note_uncertain(value: AddNoteOutput, failure: Failure) -> Case[AddNoteOutput]:
    return Case.uncertain(value, failure)

setattr(add_note_uncertain, '__module__', "dex_connectors.gorgias.gorgiasmock")

def add_note_defect(value: AddNoteOutput, failure: Failure | None = None) -> Case[AddNoteOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_note_defect, '__module__', "dex_connectors.gorgias.gorgiasmock")

def add_note_retry(failure: Failure, after_seconds: float = 0) -> Case[AddNoteOutput]:
    return Case.retry(failure, after_seconds)

setattr(add_note_retry, '__module__', "dex_connectors.gorgias.gorgiasmock")

def add_note_billing_triage_note() -> Case[AddNoteOutput]:
    return Case.branch_result(ADDED, AddNoteOutput(message=TicketMessage(id=9001, ticket_id=7001, channel='internal-note', via='api', is_public=False, is_from_agent=True, body='Follow-up triage', created_at=datetime.fromisoformat("2026-10-26T09:05:00Z".replace('Z', '+00:00'))), was_created_by_earlier_attempt=False))

setattr(add_note_billing_triage_note, '__module__', "dex_connectors.gorgias.gorgiasmock")

def add_note_not_found_failure() -> Case[AddNoteOutput]:
    return Case.branch_result(NOT_FOUND, AddNoteOutput(message=TicketMessage()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addNote", "The ticket does not exist or is not visible to the connection's user; nothing was added."))

setattr(add_note_not_found_failure, '__module__', "dex_connectors.gorgias.gorgiasmock")

def add_note_provider_rejected_failure() -> Case[AddNoteOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AddNoteOutput(message=TicketMessage()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "addNote", "Gorgias denied the requested operation."))

setattr(add_note_provider_rejected_failure, '__module__', "dex_connectors.gorgias.gorgiasmock")


class Mock:

    def __init__(self, connection_name: str = "gorgias") -> None:
        self.search_tickets_script: Query[SearchTicketsInput, SearchTicketsOutput] = Query(SEARCH_TICKETS_DEFINITION)
        self.search_tickets_script.default(search_tickets_open_billing_ticket())
        self.get_ticket_script: Query[GetTicketInput, TicketDetails] = Query(GET_TICKET_DEFINITION)
        self.get_ticket_script.default(get_ticket_billing_ticket_details())
        self.find_customer_by_email_script: Query[FindCustomerByEmailInput, CustomerMatches] = Query(FIND_CUSTOMER_BY_EMAIL_DEFINITION)
        self.find_customer_by_email_script.default(find_customer_by_email_ada_customer())
        self.create_ticket_script: Mutation[CreateTicketInput, CreateTicketOutput] = Mutation(CREATE_TICKET_DEFINITION)
        self.create_ticket_script.default(create_ticket_refund_ticket_created())
        self.update_ticket_script: Mutation[UpdateTicketInput, UpdateTicketOutput] = Mutation(UPDATE_TICKET_DEFINITION)
        self.update_ticket_script.default(update_ticket_billing_ticket_prioritized())
        self.add_note_script: Mutation[AddNoteInput, AddNoteOutput] = Mutation(ADD_NOTE_DEFINITION)
        self.add_note_script.default(add_note_billing_triage_note())
        self._connection = new_connection_with_operations(
            search_tickets=self.search_tickets_script,
            get_ticket=self.get_ticket_script,
            find_customer_by_email=self.find_customer_by_email_script,
            create_ticket=self.create_ticket_script,
            update_ticket=self.update_ticket_script,
            add_note=self.add_note_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.gorgias.gorgiasmock")

__all__ = [
    "Mock",
    "search_tickets_searched",
    "search_tickets_provider_rejected",
    "search_tickets_invalid_response",
    "search_tickets_defect",
    "search_tickets_retry",
    "search_tickets_open_billing_ticket",
    "search_tickets_provider_rejected_failure",
    "get_ticket_found",
    "get_ticket_not_found",
    "get_ticket_provider_rejected",
    "get_ticket_invalid_response",
    "get_ticket_defect",
    "get_ticket_retry",
    "get_ticket_billing_ticket_details",
    "get_ticket_not_found_failure",
    "get_ticket_provider_rejected_failure",
    "find_customer_by_email_found",
    "find_customer_by_email_not_found",
    "find_customer_by_email_provider_rejected",
    "find_customer_by_email_invalid_response",
    "find_customer_by_email_defect",
    "find_customer_by_email_retry",
    "find_customer_by_email_ada_customer",
    "find_customer_by_email_customer_absent",
    "find_customer_by_email_provider_rejected_failure",
    "create_ticket_created",
    "create_ticket_provider_rejected",
    "create_ticket_uncertain",
    "create_ticket_defect",
    "create_ticket_retry",
    "create_ticket_refund_ticket_created",
    "create_ticket_provider_rejected_failure",
    "update_ticket_updated",
    "update_ticket_not_found",
    "update_ticket_provider_rejected",
    "update_ticket_invalid_response",
    "update_ticket_defect",
    "update_ticket_retry",
    "update_ticket_billing_ticket_prioritized",
    "update_ticket_not_found_failure",
    "update_ticket_provider_rejected_failure",
    "add_note_added",
    "add_note_not_found",
    "add_note_provider_rejected",
    "add_note_uncertain",
    "add_note_defect",
    "add_note_retry",
    "add_note_billing_triage_note",
    "add_note_not_found_failure",
    "add_note_provider_rejected_failure",
]
