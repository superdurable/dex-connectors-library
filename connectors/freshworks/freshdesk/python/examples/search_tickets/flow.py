# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Search one Freshdesk ticket page with a statically named connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.freshworks_freshdesk import (
    Connection,
    SearchTicketsInput,
    SearchTicketsOutput,
    new_search_tickets_step,
)

CONNECTION_NAME = "freshdesk"


# dex:group group-id:freshdesk-search group-label:"Freshdesk search"
# dex:explanation text:"Complete with the matching Freshdesk tickets and next page."
class _Searched(dex.Step[QueryResult[SearchTicketsOutput]]):
    def get_step_type(self) -> str:
        return "FreshdeskTicketsSearched"

    def execute(self, context: dex.Context, input: QueryResult[SearchTicketsOutput]) -> dex.StepDecision:
        return dex.graceful_complete(input.value)


class SearchTicketsFlow(dex.Flow[SearchTicketsInput]):
    """Search one page through the static Freshdesk connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Freshdesk search needs the static freshdesk connection")
        self._searched = _Searched()
        # dex:group group-id:freshdesk-search group-label:"Freshdesk search"
        # dex:explanation text:"Send one filter query through the Freshdesk connection."
        self._search = new_search_tickets_step(
            step_type="FreshdeskSearchTickets",
            step_input_type=SearchTicketsInput,
            annotations={
                "groupId": "freshdesk-search",
                "groupLabel": "Freshdesk search",
                "explanation": "Search Freshdesk tickets using status, priority, tag, and date filters.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: request,
            searched=self._searched,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "FreshdeskPythonSearchTickets"

    def get_steps(self) -> dex.StepList[SearchTicketsInput]:
        """Register the provider query and success continuation."""
        return dex.StepList.start_step(self._search).other_steps(self._searched)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Return the empty schema for this stateless Flow."""
        return dex.PersistenceSchema.of()
