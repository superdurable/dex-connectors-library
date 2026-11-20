# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Udesk listTicketStatuses Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.udesk import (
    Connection,
    ListTicketStatusesInput,
    ListTicketStatusesOutput,
    new_list_ticket_statuses_step,
)

CONNECTION_NAME = "udesk"
_RESULT = dex.Attribute("udesk-result", ListTicketStatusesOutput)


def map_to_operation_input(request: ListTicketStatusesInput) -> ListTicketStatusesInput:
    """Forward the provider request."""
    return request


# dex:group group-id:udesk group-label:"Udesk"
# dex:explanation text:"Start the Udesk query."
class _StartQuery(dex.Step[ListTicketStatusesInput]):
    def execute(self, context: dex.Context, input: ListTicketStatusesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:udesk group-label:"Udesk"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListTicketStatusesOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListTicketStatusesOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListTicketStatusesFlow(dex.Flow[ListTicketStatusesInput]):
    """Run one Udesk query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Udesk Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_ticket_statuses_step(
            step_type="CallProvider",
            step_input_type=ListTicketStatusesInput,
            annotations={
                "groupId": "udesk",
                "groupLabel": "Udesk",
                "explanation": "Run Udesk listTicketStatuses.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListTicketStatusesUdesk"

    def get_steps(self) -> dex.StepList[ListTicketStatusesInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
