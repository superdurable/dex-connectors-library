# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Search one Zoho Desk ticket page with a statically named connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.zoho_desk import (
    Connection,
    SearchTicketsInput,
    SearchTicketsOutput,
    new_search_tickets_step,
)

CONNECTION_NAME = "zoho-desk-helpdesk"


# dex:group group-id:zoho-desk-helpdesk-search group-label:"Zoho Desk search"
# dex:explanation text:"Complete with the matching Zoho Desk tickets and next page."
class _Searched(dex.Step[QueryResult[SearchTicketsOutput]]):
    def get_step_type(self) -> str:
        return "ZohoDeskTicketsSearched"

    def execute(self, context: dex.Context, input: QueryResult[SearchTicketsOutput]) -> dex.StepDecision:
        return dex.graceful_complete(input.value)


class SearchTicketsFlow(dex.Flow[SearchTicketsInput]):
    """Search one page through the static Zoho Desk connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Zoho Desk search needs the static zoho-desk-helpdesk connection")
        self._searched = _Searched()
        # dex:group group-id:zoho-desk-helpdesk-search group-label:"Zoho Desk search"
        # dex:explanation text:"Send one filter query through the Zoho Desk connection."
        self._search = new_search_tickets_step(
            step_type="ZohoDeskSearchTickets",
            step_input_type=SearchTicketsInput,
            annotations={
                "groupId": "zoho-desk-helpdesk-search",
                "groupLabel": "Zoho Desk search",
                "explanation": "Search Zoho Desk tickets using status, priority, and date filters.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: request,
            searched=self._searched,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "ZohoDeskPythonSearchTickets"

    def get_steps(self) -> dex.StepList[SearchTicketsInput]:
        """Register the provider query and success continuation."""
        return dex.StepList.start_step(self._search).other_steps(self._searched)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Return the empty schema for this stateless Flow."""
        return dex.PersistenceSchema.of()
