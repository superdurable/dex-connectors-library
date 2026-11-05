# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AddCommentInput, AddCommentOutput, CreateTicketInput, CreateTicketOutput, GetTicketInput, SearchTicketsInput, SearchTicketsOutput, Ticket, TicketComment, TicketContact, TicketContactInput, TicketDetails, TicketThread, UpdateTicketInput, UpdateTicketOutput
from .definitions import CONNECTOR_ID, PROVIDER, ADDED, ADD_COMMENT_DEFINITION, CREATED, CREATE_TICKET_DEFINITION, DEFECT, FOUND, GET_TICKET_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, SEARCHED, SEARCH_TICKETS_DEFINITION, UNCERTAIN, UPDATED, UPDATE_TICKET_DEFINITION

def search_tickets_searched(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_tickets_searched, '__module__', "dex_connectors.zoho_desk.deskmock")

def search_tickets_provider_rejected(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_tickets_provider_rejected, '__module__', "dex_connectors.zoho_desk.deskmock")

def search_tickets_invalid_response(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_tickets_invalid_response, '__module__', "dex_connectors.zoho_desk.deskmock")

def search_tickets_defect(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_tickets_defect, '__module__', "dex_connectors.zoho_desk.deskmock")

def search_tickets_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchTicketsOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_tickets_retry, '__module__', "dex_connectors.zoho_desk.deskmock")

def search_tickets_no_existing_ticket() -> Case[SearchTicketsOutput]:
    return Case.paged(SEARCHED, (SearchTicketsOutput(tickets=(), total_matched=51, next_from=25), SearchTicketsOutput(tickets=(), total_matched=51, next_from=50), SearchTicketsOutput(tickets=(), total_matched=51)))

setattr(search_tickets_no_existing_ticket, '__module__', "dex_connectors.zoho_desk.deskmock")

def search_tickets_search_scope_rejected() -> Case[SearchTicketsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchTicketsOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "searchTickets", "Zoho Desk refused the search (HTTP 403)."))

setattr(search_tickets_search_scope_rejected, '__module__', "dex_connectors.zoho_desk.deskmock")

def search_tickets_invalid_search_page() -> Case[SearchTicketsOutput]:
    return Case.branch_result(INVALID_RESPONSE, SearchTicketsOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "searchTickets", "Zoho Desk returned an invalid search page."))

setattr(search_tickets_invalid_search_page, '__module__', "dex_connectors.zoho_desk.deskmock")

def get_ticket_found(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_ticket_found, '__module__', "dex_connectors.zoho_desk.deskmock")

def get_ticket_not_found(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_ticket_not_found, '__module__', "dex_connectors.zoho_desk.deskmock")

def get_ticket_provider_rejected(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_ticket_provider_rejected, '__module__', "dex_connectors.zoho_desk.deskmock")

def get_ticket_invalid_response(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_ticket_invalid_response, '__module__', "dex_connectors.zoho_desk.deskmock")

def get_ticket_defect(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_ticket_defect, '__module__', "dex_connectors.zoho_desk.deskmock")

def get_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[TicketDetails]:
    return Case.retry(failure, after_seconds)

setattr(get_ticket_retry, '__module__', "dex_connectors.zoho_desk.deskmock")

def get_ticket_opened_ticket_details() -> Case[TicketDetails]:
    return Case.branch_result(FOUND, TicketDetails(ticket=Ticket(id='1892000000099001', ticket_number='101', subject='Billing question', description_html='<div>Please check my invoice.</div>', status='On Hold', status_type='On Hold', priority='Low', channel='Web', department_id='1892000000006907', contact_id='1892000000042032', email='jane@example.test', comment_count=1, created_at=datetime.fromisoformat("2026-01-28T09:00:00Z".replace('Z', '+00:00')), modified_at=datetime.fromisoformat("2026-01-28T09:03:00Z".replace('Z', '+00:00'))), contact=TicketContact(id='1892000000042032', first_name='Jane', last_name='Doe', email='jane@example.test'), threads=(), comments=(TicketComment(id='1892000000099002', is_public=False, content='Dex triage: opened with priority High; the contact had no open or on-hold ticket in this department.', content_type='plainText', commenter_id='1892000000056007', commented_at=datetime.fromisoformat("2026-01-28T09:02:00Z".replace('Z', '+00:00'))),)))

setattr(get_ticket_opened_ticket_details, '__module__', "dex_connectors.zoho_desk.deskmock")

def get_ticket_ticket_not_found() -> Case[TicketDetails]:
    return Case.branch_result(NOT_FOUND, TicketDetails(ticket=Ticket(status='')), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getTicket", "Zoho Desk found no such ticket (HTTP 404)."))

setattr(get_ticket_ticket_not_found, '__module__', "dex_connectors.zoho_desk.deskmock")

def get_ticket_ticket_read_scope_rejected() -> Case[TicketDetails]:
    return Case.branch_result(PROVIDER_REJECTED, TicketDetails(ticket=Ticket(status='')), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getTicket", "Zoho Desk refused the ticket read (HTTP 403)."))

setattr(get_ticket_ticket_read_scope_rejected, '__module__', "dex_connectors.zoho_desk.deskmock")

def get_ticket_invalid_ticket_details() -> Case[TicketDetails]:
    return Case.branch_result(INVALID_RESPONSE, TicketDetails(ticket=Ticket(status='')), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getTicket", "Zoho Desk returned invalid ticket details."))

setattr(get_ticket_invalid_ticket_details, '__module__', "dex_connectors.zoho_desk.deskmock")

def create_ticket_created(value: CreateTicketOutput, failure: Failure | None = None) -> Case[CreateTicketOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_ticket_created, '__module__', "dex_connectors.zoho_desk.deskmock")

def create_ticket_provider_rejected(value: CreateTicketOutput, failure: Failure | None = None) -> Case[CreateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_ticket_provider_rejected, '__module__', "dex_connectors.zoho_desk.deskmock")

def create_ticket_uncertain(value: CreateTicketOutput, failure: Failure) -> Case[CreateTicketOutput]:
    return Case.uncertain(value, failure)

setattr(create_ticket_uncertain, '__module__', "dex_connectors.zoho_desk.deskmock")

def create_ticket_defect(value: CreateTicketOutput, failure: Failure | None = None) -> Case[CreateTicketOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_ticket_defect, '__module__', "dex_connectors.zoho_desk.deskmock")

def create_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateTicketOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_ticket_retry, '__module__', "dex_connectors.zoho_desk.deskmock")

def create_ticket_created_billing_ticket() -> Case[CreateTicketOutput]:
    return Case.branch_result(CREATED, CreateTicketOutput(ticket=Ticket(id='1892000000099001', ticket_number='101', subject='Billing question', description_html='<div>Please check my invoice.</div>', status='Open', status_type='Open', priority='High', channel='Web', department_id='1892000000006907', contact_id='1892000000042032', email='jane@example.test', created_at=datetime.fromisoformat("2026-01-28T09:00:00Z".replace('Z', '+00:00')), modified_at=datetime.fromisoformat("2026-01-28T09:00:00Z".replace('Z', '+00:00')))))

setattr(create_ticket_created_billing_ticket, '__module__', "dex_connectors.zoho_desk.deskmock")

def create_ticket_invalid_department() -> Case[CreateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateTicketOutput(ticket=Ticket(status='')), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "createTicket", "Zoho Desk rejected the ticket (HTTP 422)."))

setattr(create_ticket_invalid_department, '__module__', "dex_connectors.zoho_desk.deskmock")

def update_ticket_updated(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_ticket_updated, '__module__', "dex_connectors.zoho_desk.deskmock")

def update_ticket_not_found(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_ticket_not_found, '__module__', "dex_connectors.zoho_desk.deskmock")

def update_ticket_provider_rejected(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_ticket_provider_rejected, '__module__', "dex_connectors.zoho_desk.deskmock")

def update_ticket_invalid_response(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_ticket_invalid_response, '__module__', "dex_connectors.zoho_desk.deskmock")

def update_ticket_defect(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_ticket_defect, '__module__', "dex_connectors.zoho_desk.deskmock")

def update_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateTicketOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_ticket_retry, '__module__', "dex_connectors.zoho_desk.deskmock")

def update_ticket_reprioritized_billing_ticket() -> Case[UpdateTicketOutput]:
    return Case.branch_result(UPDATED, UpdateTicketOutput(ticket=Ticket(id='1892000000099001', ticket_number='101', subject='Billing question', description_html='<div>Please check my invoice.</div>', status='Open', status_type='Open', priority='High', channel='Web', department_id='1892000000006907', contact_id='1892000000042032', email='jane@example.test', comment_count=1, created_at=datetime.fromisoformat("2026-01-28T09:00:00Z".replace('Z', '+00:00')), modified_at=datetime.fromisoformat("2026-01-28T09:04:00Z".replace('Z', '+00:00')))))

setattr(update_ticket_reprioritized_billing_ticket, '__module__', "dex_connectors.zoho_desk.deskmock")

def update_ticket_ticket_not_found() -> Case[UpdateTicketOutput]:
    return Case.branch_result(NOT_FOUND, UpdateTicketOutput(ticket=Ticket(status='')), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateTicket", "Zoho Desk found no such ticket (HTTP 404)."))

setattr(update_ticket_ticket_not_found, '__module__', "dex_connectors.zoho_desk.deskmock")

def update_ticket_invalid_status() -> Case[UpdateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateTicketOutput(ticket=Ticket(id='1892000000099001', ticket_number='101', subject='Billing question', description_html='<div>Please check my invoice.</div>', status='On Hold', status_type='On Hold', priority='Low', channel='Web', department_id='1892000000006907', contact_id='1892000000042032', email='jane@example.test', comment_count=1, created_at=datetime.fromisoformat("2026-01-28T09:00:00Z".replace('Z', '+00:00')), modified_at=datetime.fromisoformat("2026-01-28T09:03:00Z".replace('Z', '+00:00')))), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "updateTicket", "Zoho Desk rejected the ticket update (HTTP 422)."))

setattr(update_ticket_invalid_status, '__module__', "dex_connectors.zoho_desk.deskmock")

def update_ticket_invalid_updated_ticket() -> Case[UpdateTicketOutput]:
    return Case.branch_result(INVALID_RESPONSE, UpdateTicketOutput(ticket=Ticket(id='1892000000099001', ticket_number='101', subject='Billing question', description_html='<div>Please check my invoice.</div>', status='On Hold', status_type='On Hold', priority='Low', channel='Web', department_id='1892000000006907', contact_id='1892000000042032', email='jane@example.test', comment_count=1, created_at=datetime.fromisoformat("2026-01-28T09:00:00Z".replace('Z', '+00:00')), modified_at=datetime.fromisoformat("2026-01-28T09:03:00Z".replace('Z', '+00:00')))), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "updateTicket", "Zoho Desk returned an invalid updated ticket."))

setattr(update_ticket_invalid_updated_ticket, '__module__', "dex_connectors.zoho_desk.deskmock")

def add_comment_added(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_comment_added, '__module__', "dex_connectors.zoho_desk.deskmock")

def add_comment_not_found(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_comment_not_found, '__module__', "dex_connectors.zoho_desk.deskmock")

def add_comment_provider_rejected(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_comment_provider_rejected, '__module__', "dex_connectors.zoho_desk.deskmock")

def add_comment_uncertain(value: AddCommentOutput, failure: Failure) -> Case[AddCommentOutput]:
    return Case.uncertain(value, failure)

setattr(add_comment_uncertain, '__module__', "dex_connectors.zoho_desk.deskmock")

def add_comment_defect(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_comment_defect, '__module__', "dex_connectors.zoho_desk.deskmock")

def add_comment_retry(failure: Failure, after_seconds: float = 0) -> Case[AddCommentOutput]:
    return Case.retry(failure, after_seconds)

setattr(add_comment_retry, '__module__', "dex_connectors.zoho_desk.deskmock")

def add_comment_private_triage_comment() -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, AddCommentOutput(comment=TicketComment(id='1892000000099002', is_public=False, content='Dex triage: opened with priority High; the contact had no open or on-hold ticket in this department.', content_type='plainText', commenter_id='1892000000056007', commented_at=datetime.fromisoformat("2026-01-28T09:02:00Z".replace('Z', '+00:00')))))

setattr(add_comment_private_triage_comment, '__module__', "dex_connectors.zoho_desk.deskmock")

def add_comment_ticket_not_found() -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, AddCommentOutput(comment=TicketComment()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addComment", "Zoho Desk found no such ticket (HTTP 404)."))

setattr(add_comment_ticket_not_found, '__module__', "dex_connectors.zoho_desk.deskmock")

def add_comment_invalid_comment() -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AddCommentOutput(comment=TicketComment()), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "addComment", "Zoho Desk rejected the comment (HTTP 422)."))

setattr(add_comment_invalid_comment, '__module__', "dex_connectors.zoho_desk.deskmock")


class Mock:

    def __init__(self, connection_name: str = "zoho-desk") -> None:
        self.search_tickets_script: Query[SearchTicketsInput, SearchTicketsOutput] = Query(SEARCH_TICKETS_DEFINITION)
        self.search_tickets_script.default(search_tickets_no_existing_ticket())
        self.get_ticket_script: Query[GetTicketInput, TicketDetails] = Query(GET_TICKET_DEFINITION)
        self.get_ticket_script.default(get_ticket_opened_ticket_details())
        self.create_ticket_script: Mutation[CreateTicketInput, CreateTicketOutput] = Mutation(CREATE_TICKET_DEFINITION)
        self.create_ticket_script.default(create_ticket_created_billing_ticket())
        self.update_ticket_script: Mutation[UpdateTicketInput, UpdateTicketOutput] = Mutation(UPDATE_TICKET_DEFINITION)
        self.update_ticket_script.default(update_ticket_reprioritized_billing_ticket())
        self.add_comment_script: Mutation[AddCommentInput, AddCommentOutput] = Mutation(ADD_COMMENT_DEFINITION)
        self.add_comment_script.default(add_comment_private_triage_comment())
        self._connection = new_connection_with_operations(
            search_tickets=self.search_tickets_script,
            get_ticket=self.get_ticket_script,
            create_ticket=self.create_ticket_script,
            update_ticket=self.update_ticket_script,
            add_comment=self.add_comment_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.zoho_desk.deskmock")

__all__ = [
    "Mock",
    "search_tickets_searched",
    "search_tickets_provider_rejected",
    "search_tickets_invalid_response",
    "search_tickets_defect",
    "search_tickets_retry",
    "search_tickets_no_existing_ticket",
    "search_tickets_search_scope_rejected",
    "search_tickets_invalid_search_page",
    "get_ticket_found",
    "get_ticket_not_found",
    "get_ticket_provider_rejected",
    "get_ticket_invalid_response",
    "get_ticket_defect",
    "get_ticket_retry",
    "get_ticket_opened_ticket_details",
    "get_ticket_ticket_not_found",
    "get_ticket_ticket_read_scope_rejected",
    "get_ticket_invalid_ticket_details",
    "create_ticket_created",
    "create_ticket_provider_rejected",
    "create_ticket_uncertain",
    "create_ticket_defect",
    "create_ticket_retry",
    "create_ticket_created_billing_ticket",
    "create_ticket_invalid_department",
    "update_ticket_updated",
    "update_ticket_not_found",
    "update_ticket_provider_rejected",
    "update_ticket_invalid_response",
    "update_ticket_defect",
    "update_ticket_retry",
    "update_ticket_reprioritized_billing_ticket",
    "update_ticket_ticket_not_found",
    "update_ticket_invalid_status",
    "update_ticket_invalid_updated_ticket",
    "add_comment_added",
    "add_comment_not_found",
    "add_comment_provider_rejected",
    "add_comment_uncertain",
    "add_comment_defect",
    "add_comment_retry",
    "add_comment_private_triage_comment",
    "add_comment_ticket_not_found",
    "add_comment_invalid_comment",
]
