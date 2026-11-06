# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetTicketInput, TicketDetails
from .definitions import CONNECTOR_ID, PROVIDER, DEFECT, FOUND, GET_TICKET_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

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
    return Case.branch_result(FOUND, {'ticket': {'id': '10042', 'key': 'ITH-42', 'projectKey': 'ITH', 'summary': 'Laptop will not boot', 'status': {'id': '10000', 'name': 'Waiting for support', 'categoryKey': 'new'}, 'priority': {'id': '2', 'name': 'High'}, 'reporter': {'accountId': '5b10ac8d82e05b22cc7d4ef5', 'displayName': 'Jane Doe'}, 'labels': [], 'description': 'My laptop will not boot.', 'createdAt': '2026-09-21T14:13:20Z', 'updatedAt': '2026-09-21T14:13:20Z'}, 'comments': [], 'slas': [{'id': '3', 'name': 'Time to first response', 'ongoingCycle': {'startedAt': '2026-09-21T14:13:20Z', 'goalDurationMillis': 14400000, 'elapsedMillis': 0, 'remainingMillis': 14400000}}]})

setattr(get_ticket_found_default_answer, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def get_ticket_not_found_failure() -> Case[TicketDetails]:
    return Case.branch_result(NOT_FOUND, {}, Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getTicket", "The request was not found."))

setattr(get_ticket_not_found_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def get_ticket_provider_rejected_failure() -> Case[TicketDetails]:
    return Case.branch_result(PROVIDER_REJECTED, {}, Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getTicket", "The SLA read was forbidden."))

setattr(get_ticket_provider_rejected_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

def get_ticket_invalid_response_failure() -> Case[TicketDetails]:
    return Case.branch_result(INVALID_RESPONSE, {}, Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getTicket", "The SLA page was malformed."))

setattr(get_ticket_invalid_response_failure, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")


class Mock:

    def __init__(self, connection_name: str = "jira-service-management") -> None:
        self.get_ticket_script: Query[GetTicketInput, TicketDetails] = Query(GET_TICKET_DEFINITION)
        self.get_ticket_script.default(get_ticket_found_default_answer())
        self._connection = new_connection_with_operations(
            get_ticket=self.get_ticket_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.atlassian_jira_service_management.jiraservicemanagementmock")

__all__ = [
    "Mock",
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
]
