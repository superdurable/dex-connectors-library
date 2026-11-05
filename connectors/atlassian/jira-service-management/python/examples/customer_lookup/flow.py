# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Find one service desk customer through a durable Dex Query."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.atlassian_jira_service_management import (
    Connection,
    CustomerMatches,
    FindCustomerByEmailInput,
    new_find_customer_by_email_step,
)

CONNECTION_NAME = "jsm-support"
FLOW_TYPE = "JiraServiceManagementCustomerLookupPython"


@dataclass(frozen=True, slots=True)
class CustomerLookupRequest:
    """Identify a service desk and the customer's email address."""

    service_desk_id: str
    email: str


def map_to_customer_lookup_input(request: CustomerLookupRequest) -> FindCustomerByEmailInput:
    """Pass the Flow's safe input to the connector Query."""
    return FindCustomerByEmailInput(request.service_desk_id, request.email)


# dex:group group-id:jsm-customer group-label:"Jira Service Management customer"
# dex:explanation text:"Return the matching customer accounts without saving their email in an Attribute."
class CompleteCustomerLookup(dex.Step[QueryResult[CustomerMatches]]):
    """Complete with public customer identifiers and display names."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "CompleteJiraServiceManagementCustomerLookup"

    def execute(self, context: dex.Context, input: QueryResult[CustomerMatches]) -> dex.StepDecision:
        """Complete the Flow with the connector result."""
        return dex.graceful_complete(input.value)


class CustomerLookupFlow(dex.Flow[CustomerLookupRequest]):
    """Find customers through the static Jira Service Management connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("customer lookup requires the jsm-support connection")
        self.complete = CompleteCustomerLookup()
        self.lookup = new_find_customer_by_email_step(
            step_type="FindJiraServiceManagementCustomer",
            step_input_type=CustomerLookupRequest,
            annotations={
                "groupId": "jsm-customer",
                "groupLabel": "Jira Service Management customer",
                "explanation": "Find matching customer accounts on the chosen service desk.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_customer_lookup_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[CustomerLookupRequest]:
        """Register the lookup and completion Steps."""
        return dex.StepList.start_step(self.lookup).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist no Attributes for this lookup."""
        return dex.PersistenceSchema.of()
