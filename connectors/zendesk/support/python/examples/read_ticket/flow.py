# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read a Zendesk Support ticket and its latest messages in a Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.zendesk_support import Connection, GetTicketInput, TicketDetails, new_get_ticket_step

CONNECTION_NAME = "zendesk-support-desk"
TICKET_DETAILS = dex.Attribute("zendesk-support-read-ticket-details", TicketDetails)


# dex:group group-id:zendesk-support-read group-label:"Zendesk Support triage"
# dex:explanation text:"Persist the ticket and its latest messages for support triage."
class CompleteTicketTriage(dex.Step[QueryResult[TicketDetails]]):
    """Complete the Flow with the decoded ticket details."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "CompleteTicketTriage"

    def execute(self, context: dex.Context, input: QueryResult[TicketDetails]) -> dex.StepDecision:
        """Persist the provider-safe result and complete the Flow."""
        TICKET_DETAILS.set(context, input.value)
        return dex.graceful_complete(input.value)


class ReadTicketFlow(dex.Flow[GetTicketInput]):
    """Read a Zendesk Support ticket through the static project connection."""

    def __init__(self, connection: Connection) -> None:
        if connection is None or connection.reference.name != CONNECTION_NAME:
            raise ValueError("Zendesk Support triage requires its static connection name")
        self.complete = CompleteTicketTriage()
        self.get_ticket = new_get_ticket_step(
            step_type="GetZendeskSupportTicket",
            step_input_type=GetTicketInput,
            annotations={
                "groupId": "zendesk-support-read",
                "groupLabel": "Zendesk Support triage",
                "explanation": "Read the requested ticket and its latest messages from Zendesk Support.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "ZendeskSupportReadTicketPython"

    def get_steps(self) -> dex.StepList[GetTicketInput]:
        """Register the provider read and completion Step."""
        return dex.StepList.start_step(self.get_ticket).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the persisted ticket details."""
        return dex.PersistenceSchema.of(TICKET_DETAILS)
