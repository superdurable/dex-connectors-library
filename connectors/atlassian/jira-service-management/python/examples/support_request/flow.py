# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Find a service desk customer by email through a Dex Query Step."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.atlassian_jira_service_management import (
    Connection,
    CreateTicketInput,
    CreateTicketOutput,
    CustomerMatches,
    FindCustomerByEmailInput,
    new_create_ticket_step,
    new_find_customer_by_email_step,
)

CONNECTION_NAME = "jira-service-management-support"


@dataclass(frozen=True, slots=True)
class SupportRequestInput:
    """The service desk and bare customer email to look up."""

    service_desk_id: str = field(metadata={"dex_json_name": "serviceDeskId"})
    email: str = ""

    def to_operation_input(self) -> FindCustomerByEmailInput:
        """Map safe Flow input to the customer lookup operation."""
        return FindCustomerByEmailInput(self.service_desk_id, self.email)


customer_matches = dex.Attribute("jsm-python-customer-matches", CustomerMatches)
created_ticket = dex.Attribute("jsm-python-created-ticket", CreateTicketOutput)


@dataclass(frozen=True, slots=True)
class SupportTicketInput:
    """One request type and customer-visible ticket summary."""

    service_desk_id: str = field(metadata={"dex_json_name": "serviceDeskId"})
    request_type_id: str = field(metadata={"dex_json_name": "requestTypeId"})
    summary: str
    description: str = ""

    def to_operation_input(self) -> CreateTicketInput:
        """Send only the request fields this example owns."""
        return CreateTicketInput(self.service_desk_id, self.request_type_id, self.summary, self.description)


# dex:group group-id:jsm-support group-label:"Jira Support"
# dex:explanation text:"Persist customer matches and complete the support request lookup."
class CompleteCustomerLookup(dex.Step[QueryResult[CustomerMatches]]):
    """Complete the Flow after saving only non-secret customer IDs."""

    def execute(self, context: dex.Context, input: QueryResult[CustomerMatches]) -> dex.StepDecision:
        """Persist and return the exact customer matches."""
        customer_matches.set(context, input.value)
        return dex.graceful_complete(input.value)


# dex:group group-id:jsm-support group-label:"Jira Support"
# dex:explanation text:"Persist the created support ticket reference and complete the Flow."
class CompleteTicketCreation(dex.Step[MutationResult[CreateTicketOutput]]):
    """Complete after saving the provider's ticket reference."""

    def execute(self, context: dex.Context, input: MutationResult[CreateTicketOutput]) -> dex.StepDecision:
        """Persist only the non-secret request reference."""
        created_ticket.set(context, input.value)
        return dex.graceful_complete(input.value)


class SupportRequestFlow(dex.Flow[SupportRequestInput]):
    """Find a customer in the selected Jira Service Management desk."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static project connection before Worker registration."""
        self.complete = CompleteCustomerLookup()
        self.find_customer = new_find_customer_by_email_step(
            step_type="FindJiraCustomerByEmail",
            step_input_type=SupportRequestInput,
            annotations={
                "groupId": "jsm-support",
                "groupLabel": "Jira Support",
                "explanation": "Find a service desk customer by exact email address.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=SupportRequestInput.to_operation_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Keep the example Flow identity stable."""
        return "JiraSupportRequestLookupPython"

    def get_steps(self) -> dex.StepList[SupportRequestInput]:
        """Query the provider, then complete with matches."""
        return dex.StepList.start_step(self.find_customer).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the safe customer result Attribute."""
        return dex.PersistenceSchema.of(customer_matches)


class CreateTicketFlow(dex.Flow[SupportTicketInput]):
    """Create one support request with a durable single dispatch checkpoint."""

    def __init__(self, connection: Connection) -> None:
        """Bind the create mutation to the static project connection."""
        self.complete = CompleteTicketCreation()
        self.create_ticket = new_create_ticket_step(
            step_type="CreateJiraSupportTicket",
            step_input_type=SupportTicketInput,
            annotations={
                "groupId": "jsm-support",
                "groupLabel": "Jira Support",
                "explanation": "Create one customer request without resending a possibly accepted write.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=SupportTicketInput.to_operation_input,
            created=self.complete,
        )

    def get_flow_type(self) -> str:
        """Keep the ticket creation Flow identity stable."""
        return "JiraCreateSupportTicketPython"

    def get_steps(self) -> dex.StepList[SupportTicketInput]:
        """Create the request, then persist its reference."""
        return dex.StepList.start_step(self.create_ticket).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the created request Attribute."""
        return dex.PersistenceSchema.of(created_ticket)
