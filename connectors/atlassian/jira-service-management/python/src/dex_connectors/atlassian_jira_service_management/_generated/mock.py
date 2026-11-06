# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AccountReference, AddCommentInput, AddCommentOutput, CreateTicketInput, CreateTicketOutput, Customer, CustomerMatches, FindCustomerByEmailInput, GetTicketInput, PriorityReference, RequestStatus, SLACycle, SearchTicketsInput, SearchTicketsOutput, Ticket, TicketComment, TicketDetails, TicketSLA, TicketStatus, TransitionReference, TransitionTicketInput, TransitionTicketOutput, UpdateTicketInput, UpdateTicketOutput
from .definitions import CONNECTOR_ID, PROVIDER, ADDED, ADD_COMMENT_DEFINITION, CREATED, CREATE_TICKET_DEFINITION, DEFECT, FIND_CUSTOMER_BY_EMAIL_DEFINITION, FOUND, GET_TICKET_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, SEARCHED, SEARCH_TICKETS_DEFINITION, TRANSITIONED, TRANSITION_TICKET_DEFINITION, TRANSITION_UNAVAILABLE, UNCERTAIN, UPDATED, UPDATE_TICKET_DEFINITION

def find_customer_by_email_found(value: CustomerMatches, failure: Failure | None = None) -> Case[CustomerMatches]:
    return Case.branch_result(FOUND, value, failure)

setattr(find_customer_by_email_found, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def find_customer_by_email_not_found(value: CustomerMatches, failure: Failure | None = None) -> Case[CustomerMatches]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(find_customer_by_email_not_found, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def find_customer_by_email_provider_rejected(value: CustomerMatches, failure: Failure | None = None) -> Case[CustomerMatches]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(find_customer_by_email_provider_rejected, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def find_customer_by_email_invalid_response(value: CustomerMatches, failure: Failure | None = None) -> Case[CustomerMatches]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(find_customer_by_email_invalid_response, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def find_customer_by_email_defect(value: CustomerMatches, failure: Failure | None = None) -> Case[CustomerMatches]:
    return Case.branch_result(DEFECT, value, failure)

setattr(find_customer_by_email_defect, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def find_customer_by_email_retry(failure: Failure, after_seconds: float = 0) -> Case[CustomerMatches]:
    return Case.retry(failure, after_seconds)

setattr(find_customer_by_email_retry, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def find_customer_by_email_found_default_answer() -> Case[CustomerMatches]:
    return Case.branch_result(FOUND, CustomerMatches(email='jane@acme.example.com', customers=(Customer(account_id='5b10ac8d82e05b22cc7d4ef5', display_name='Jane Doe', is_active=True),)))

setattr(find_customer_by_email_found_default_answer, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def find_customer_by_email_not_found_failure() -> Case[CustomerMatches]:
    return Case.branch_result(NOT_FOUND, CustomerMatches(email='nobody@acme.example.com', customers=()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "findCustomerByEmail", "No customer has that email."))

setattr(find_customer_by_email_not_found_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def find_customer_by_email_provider_rejected_failure() -> Case[CustomerMatches]:
    return Case.branch_result(PROVIDER_REJECTED, CustomerMatches(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "findCustomerByEmail", "Customer lookup was forbidden."))

setattr(find_customer_by_email_provider_rejected_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def find_customer_by_email_invalid_response_failure() -> Case[CustomerMatches]:
    return Case.branch_result(INVALID_RESPONSE, CustomerMatches(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "findCustomerByEmail", "The customer page was malformed."))

setattr(find_customer_by_email_invalid_response_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def search_tickets_searched(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_tickets_searched, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def search_tickets_provider_rejected(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_tickets_provider_rejected, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def search_tickets_invalid_response(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_tickets_invalid_response, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def search_tickets_defect(value: SearchTicketsOutput, failure: Failure | None = None) -> Case[SearchTicketsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_tickets_defect, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def search_tickets_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchTicketsOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_tickets_retry, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def search_tickets_searched_default_answer() -> Case[SearchTicketsOutput]:
    return Case.paged(SEARCHED, (SearchTicketsOutput(jql='project = ITH AND reporter = "5b10ac8d82e05b22cc7d4ef5"', tickets=(Ticket(id='10040', key='ITH-40', project_key='ITH', summary='Laptop will not boot after an update', status=TicketStatus(id='10000', name='Waiting for support', category_key='new'), created_at=datetime.fromisoformat("2026-09-20T14:13:20Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-09-20T14:13:20Z".replace('Z', '+00:00'))),), next_page_token='page-2'), SearchTicketsOutput(jql='project = ITH AND reporter = "5b10ac8d82e05b22cc7d4ef5"', tickets=(Ticket(id='10041', key='ITH-41', project_key='ITH', summary='Laptop will not boot on Monday', status=TicketStatus(id='10000', name='Waiting for support', category_key='new'), created_at=datetime.fromisoformat("2026-09-19T14:13:20Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-09-19T14:13:20Z".replace('Z', '+00:00'))), Ticket(id='10040', key='ITH-40', project_key='ITH', summary='Laptop will not boot after an update', status=TicketStatus(id='10000', name='Waiting for support', category_key='new'), created_at=datetime.fromisoformat("2026-09-20T14:13:20Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-09-20T14:13:20Z".replace('Z', '+00:00')))), next_page_token='page-3'), SearchTicketsOutput(jql='project = ITH AND reporter = "5b10ac8d82e05b22cc7d4ef5"', tickets=(), next_page_token='')))

setattr(search_tickets_searched_default_answer, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def search_tickets_provider_rejected_failure() -> Case[SearchTicketsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchTicketsOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "searchTickets", "Issue search was forbidden."))

setattr(search_tickets_provider_rejected_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def search_tickets_invalid_response_failure() -> Case[SearchTicketsOutput]:
    return Case.branch_result(INVALID_RESPONSE, SearchTicketsOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "searchTickets", "The search page was malformed."))

setattr(search_tickets_invalid_response_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def get_ticket_found(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_ticket_found, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def get_ticket_not_found(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_ticket_not_found, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def get_ticket_provider_rejected(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_ticket_provider_rejected, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def get_ticket_invalid_response(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_ticket_invalid_response, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def get_ticket_defect(value: TicketDetails, failure: Failure | None = None) -> Case[TicketDetails]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_ticket_defect, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def get_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[TicketDetails]:
    return Case.retry(failure, after_seconds)

setattr(get_ticket_retry, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def get_ticket_found_default_answer() -> Case[TicketDetails]:
    return Case.branch_result(FOUND, TicketDetails(ticket=Ticket(id='10042', key='ITH-42', project_key='ITH', summary='Laptop will not boot', status=TicketStatus(id='10000', name='Waiting for support', category_key='new'), priority=PriorityReference(id='2', name='High'), reporter=AccountReference(account_id='5b10ac8d82e05b22cc7d4ef5', display_name='Jane Doe'), labels=(), description='My laptop will not boot.', created_at=datetime.fromisoformat("2026-09-21T14:13:20Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-09-21T14:13:20Z".replace('Z', '+00:00'))), comments=(), slas=(TicketSLA(id='3', name='Time to first response', ongoing_cycle=SLACycle(started_at=datetime.fromisoformat("2026-09-21T14:13:20Z".replace('Z', '+00:00')), goal_duration_millis=14400000, elapsed_millis=0, remaining_millis=14400000)),)))

setattr(get_ticket_found_default_answer, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def get_ticket_not_found_failure() -> Case[TicketDetails]:
    return Case.branch_result(NOT_FOUND, TicketDetails(ticket=Ticket(status=TicketStatus())), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getTicket", "The request was not found."))

setattr(get_ticket_not_found_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def get_ticket_provider_rejected_failure() -> Case[TicketDetails]:
    return Case.branch_result(PROVIDER_REJECTED, TicketDetails(ticket=Ticket(status=TicketStatus())), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getTicket", "The SLA read was forbidden."))

setattr(get_ticket_provider_rejected_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def get_ticket_invalid_response_failure() -> Case[TicketDetails]:
    return Case.branch_result(INVALID_RESPONSE, TicketDetails(ticket=Ticket(status=TicketStatus())), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getTicket", "The SLA page was malformed."))

setattr(get_ticket_invalid_response_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def create_ticket_created(value: CreateTicketOutput, failure: Failure | None = None) -> Case[CreateTicketOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_ticket_created, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def create_ticket_provider_rejected(value: CreateTicketOutput, failure: Failure | None = None) -> Case[CreateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_ticket_provider_rejected, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def create_ticket_uncertain(value: CreateTicketOutput, failure: Failure) -> Case[CreateTicketOutput]:
    return Case.uncertain(value, failure)

setattr(create_ticket_uncertain, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def create_ticket_defect(value: CreateTicketOutput, failure: Failure | None = None) -> Case[CreateTicketOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_ticket_defect, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def create_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateTicketOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_ticket_retry, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def create_ticket_created_default_answer() -> Case[CreateTicketOutput]:
    return Case.branch_result(CREATED, CreateTicketOutput(issue_id='10042', issue_key='ITH-42', service_desk_id='10', request_type_id='25', summary='Laptop will not boot', raise_on_behalf_of_account_id='5b10ac8d82e05b22cc7d4ef5', request_status=RequestStatus(name='Waiting for support', category='NEW'), created_at=datetime.fromisoformat("2026-09-21T14:13:20Z".replace('Z', '+00:00'))))

setattr(create_ticket_created_default_answer, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def create_ticket_provider_rejected_failure() -> Case[CreateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateTicketOutput(service_desk_id='10', request_type_id='25', summary='Laptop will not boot', rejected_field_ids=('customfield_10050',), provider_error_key='sd.request.create.field.required'), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "createTicket", "A required request field was missing."))

setattr(create_ticket_provider_rejected_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def update_ticket_updated(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_ticket_updated, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def update_ticket_not_found(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_ticket_not_found, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def update_ticket_provider_rejected(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_ticket_provider_rejected, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def update_ticket_invalid_response(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_ticket_invalid_response, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def update_ticket_defect(value: UpdateTicketOutput, failure: Failure | None = None) -> Case[UpdateTicketOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_ticket_defect, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def update_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateTicketOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_ticket_retry, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def update_ticket_updated_default_answer() -> Case[UpdateTicketOutput]:
    return Case.branch_result(UPDATED, UpdateTicketOutput(issue_id='10042', issue_key='ITH-42', labels=('hardware',), priority=PriorityReference(id='2', name='High')))

setattr(update_ticket_updated_default_answer, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def update_ticket_not_found_failure() -> Case[UpdateTicketOutput]:
    return Case.branch_result(NOT_FOUND, UpdateTicketOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateTicket", "The issue was not found."))

setattr(update_ticket_not_found_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def update_ticket_provider_rejected_failure() -> Case[UpdateTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateTicketOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "updateTicket", "The issue update was forbidden."))

setattr(update_ticket_provider_rejected_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def update_ticket_invalid_response_failure() -> Case[UpdateTicketOutput]:
    return Case.branch_result(INVALID_RESPONSE, UpdateTicketOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "updateTicket", "The read-back issue was malformed."))

setattr(update_ticket_invalid_response_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def transition_ticket_transitioned(value: TransitionTicketOutput, failure: Failure | None = None) -> Case[TransitionTicketOutput]:
    return Case.branch_result(TRANSITIONED, value, failure)

setattr(transition_ticket_transitioned, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def transition_ticket_not_found(value: TransitionTicketOutput, failure: Failure | None = None) -> Case[TransitionTicketOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(transition_ticket_not_found, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def transition_ticket_transition_unavailable(value: TransitionTicketOutput, failure: Failure | None = None) -> Case[TransitionTicketOutput]:
    return Case.branch_result(TRANSITION_UNAVAILABLE, value, failure)

setattr(transition_ticket_transition_unavailable, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def transition_ticket_provider_rejected(value: TransitionTicketOutput, failure: Failure | None = None) -> Case[TransitionTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(transition_ticket_provider_rejected, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def transition_ticket_invalid_response(value: TransitionTicketOutput, failure: Failure | None = None) -> Case[TransitionTicketOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(transition_ticket_invalid_response, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def transition_ticket_defect(value: TransitionTicketOutput, failure: Failure | None = None) -> Case[TransitionTicketOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(transition_ticket_defect, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def transition_ticket_retry(failure: Failure, after_seconds: float = 0) -> Case[TransitionTicketOutput]:
    return Case.retry(failure, after_seconds)

setattr(transition_ticket_retry, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def transition_ticket_transitioned_default_answer() -> Case[TransitionTicketOutput]:
    return Case.branch_result(TRANSITIONED, TransitionTicketOutput(issue_id='10042', issue_key='ITH-42', previous_status=TicketStatus(id='10000', name='Waiting for support', category_key='new'), status=TicketStatus(id='10001', name='In progress', category_key='indeterminate'), transition=TransitionReference(id='31', name='Start Progress', destination_status=TicketStatus(id='10001', name='In progress', category_key='indeterminate'))))

setattr(transition_ticket_transitioned_default_answer, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def transition_ticket_not_found_failure() -> Case[TransitionTicketOutput]:
    return Case.branch_result(NOT_FOUND, TransitionTicketOutput(previous_status=TicketStatus(), status=TicketStatus()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "transitionTicket", "The issue was not found."))

setattr(transition_ticket_not_found_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def transition_ticket_transition_unavailable_failure() -> Case[TransitionTicketOutput]:
    return Case.branch_result(TRANSITION_UNAVAILABLE, TransitionTicketOutput(issue_id='10042', issue_key='ITH-42', previous_status=TicketStatus(id='10000', name='Waiting for support', category_key='new'), status=TicketStatus(id='10000', name='Waiting for support', category_key='new'), available_transitions=(TransitionReference(id='31', name='Start Progress', destination_status=TicketStatus(id='10001', name='In progress', category_key='indeterminate')),)), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "transitionTicket", "No transition to Closed is available."))

setattr(transition_ticket_transition_unavailable_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def transition_ticket_provider_rejected_failure() -> Case[TransitionTicketOutput]:
    return Case.branch_result(PROVIDER_REJECTED, TransitionTicketOutput(previous_status=TicketStatus(), status=TicketStatus()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "transitionTicket", "The transition was forbidden."))

setattr(transition_ticket_provider_rejected_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def transition_ticket_invalid_response_failure() -> Case[TransitionTicketOutput]:
    return Case.branch_result(INVALID_RESPONSE, TransitionTicketOutput(previous_status=TicketStatus(), status=TicketStatus()), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "transitionTicket", "The transition list was malformed."))

setattr(transition_ticket_invalid_response_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def add_comment_added(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_comment_added, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def add_comment_not_found(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_comment_not_found, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def add_comment_provider_rejected(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_comment_provider_rejected, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def add_comment_uncertain(value: AddCommentOutput, failure: Failure) -> Case[AddCommentOutput]:
    return Case.uncertain(value, failure)

setattr(add_comment_uncertain, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def add_comment_defect(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_comment_defect, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def add_comment_retry(failure: Failure, after_seconds: float = 0) -> Case[AddCommentOutput]:
    return Case.retry(failure, after_seconds)

setattr(add_comment_retry, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def add_comment_added_default_answer() -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, AddCommentOutput(issue_id_or_key='ITH-42', comment_id='1001', is_public=False, created_at=datetime.fromisoformat("2026-09-21T14:15:00Z".replace('Z', '+00:00'))))

setattr(add_comment_added_default_answer, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def add_comment_not_found_failure() -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, AddCommentOutput(issue_id_or_key='ITH-99', is_public=False), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addComment", "The request was not found."))

setattr(add_comment_not_found_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def add_comment_provider_rejected_failure() -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AddCommentOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "addComment", "Adding the comment was forbidden."))

setattr(add_comment_provider_rejected_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")


class Mock:

    def __init__(self, connection_name: str = "jira-service-management") -> None:
        self.find_customer_by_email_script: Query[FindCustomerByEmailInput, CustomerMatches] = Query(FIND_CUSTOMER_BY_EMAIL_DEFINITION)
        self.find_customer_by_email_script.default(find_customer_by_email_found_default_answer())
        self.search_tickets_script: Query[SearchTicketsInput, SearchTicketsOutput] = Query(SEARCH_TICKETS_DEFINITION)
        self.search_tickets_script.default(search_tickets_searched_default_answer())
        self.get_ticket_script: Query[GetTicketInput, TicketDetails] = Query(GET_TICKET_DEFINITION)
        self.get_ticket_script.default(get_ticket_found_default_answer())
        self.create_ticket_script: Mutation[CreateTicketInput, CreateTicketOutput] = Mutation(CREATE_TICKET_DEFINITION)
        self.create_ticket_script.default(create_ticket_created_default_answer())
        self.update_ticket_script: Mutation[UpdateTicketInput, UpdateTicketOutput] = Mutation(UPDATE_TICKET_DEFINITION)
        self.update_ticket_script.default(update_ticket_updated_default_answer())
        self.transition_ticket_script: Mutation[TransitionTicketInput, TransitionTicketOutput] = Mutation(TRANSITION_TICKET_DEFINITION)
        self.transition_ticket_script.default(transition_ticket_transitioned_default_answer())
        self.add_comment_script: Mutation[AddCommentInput, AddCommentOutput] = Mutation(ADD_COMMENT_DEFINITION)
        self.add_comment_script.default(add_comment_added_default_answer())
        self._connection = new_connection_with_operations(
            find_customer_by_email=self.find_customer_by_email_script,
            search_tickets=self.search_tickets_script,
            get_ticket=self.get_ticket_script,
            create_ticket=self.create_ticket_script,
            update_ticket=self.update_ticket_script,
            transition_ticket=self.transition_ticket_script,
            add_comment=self.add_comment_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

__all__ = [
    "Mock",
    "find_customer_by_email_found",
    "find_customer_by_email_not_found",
    "find_customer_by_email_provider_rejected",
    "find_customer_by_email_invalid_response",
    "find_customer_by_email_defect",
    "find_customer_by_email_retry",
    "find_customer_by_email_found_default_answer",
    "find_customer_by_email_not_found_failure",
    "find_customer_by_email_provider_rejected_failure",
    "find_customer_by_email_invalid_response_failure",
    "search_tickets_searched",
    "search_tickets_provider_rejected",
    "search_tickets_invalid_response",
    "search_tickets_defect",
    "search_tickets_retry",
    "search_tickets_searched_default_answer",
    "search_tickets_provider_rejected_failure",
    "search_tickets_invalid_response_failure",
    "get_ticket_found",
    "get_ticket_not_found",
    "get_ticket_provider_rejected",
    "get_ticket_invalid_response",
    "get_ticket_defect",
    "get_ticket_retry",
    "get_ticket_found_default_answer",
    "get_ticket_not_found_failure",
    "get_ticket_provider_rejected_failure",
    "get_ticket_invalid_response_failure",
    "create_ticket_created",
    "create_ticket_provider_rejected",
    "create_ticket_uncertain",
    "create_ticket_defect",
    "create_ticket_retry",
    "create_ticket_created_default_answer",
    "create_ticket_provider_rejected_failure",
    "update_ticket_updated",
    "update_ticket_not_found",
    "update_ticket_provider_rejected",
    "update_ticket_invalid_response",
    "update_ticket_defect",
    "update_ticket_retry",
    "update_ticket_updated_default_answer",
    "update_ticket_not_found_failure",
    "update_ticket_provider_rejected_failure",
    "update_ticket_invalid_response_failure",
    "transition_ticket_transitioned",
    "transition_ticket_not_found",
    "transition_ticket_transition_unavailable",
    "transition_ticket_provider_rejected",
    "transition_ticket_invalid_response",
    "transition_ticket_defect",
    "transition_ticket_retry",
    "transition_ticket_transitioned_default_answer",
    "transition_ticket_not_found_failure",
    "transition_ticket_transition_unavailable_failure",
    "transition_ticket_provider_rejected_failure",
    "transition_ticket_invalid_response_failure",
    "add_comment_added",
    "add_comment_not_found",
    "add_comment_provider_rejected",
    "add_comment_uncertain",
    "add_comment_defect",
    "add_comment_retry",
    "add_comment_added_default_answer",
    "add_comment_not_found_failure",
    "add_comment_provider_rejected_failure",
]
