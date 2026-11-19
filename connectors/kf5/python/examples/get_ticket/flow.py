# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable KF5 getTicket Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.kf5 import Connection, GetTicketInput, GetTicketOutput, new_get_ticket_step

CONNECTION_NAME = "kf5"
_RESULT = dex.Attribute("kf5-result", GetTicketOutput)


def map_to_operation_input(request: GetTicketInput) -> GetTicketInput:
    """Forward the provider request."""
    return request


# dex:group group-id:kf5 group-label:"KF5"
# dex:explanation text:"Start the KF5 query."
class _StartQuery(dex.Step[GetTicketInput]):
    def execute(self, context: dex.Context, input: GetTicketInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:kf5 group-label:"KF5"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[GetTicketOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[GetTicketOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetTicketFlow(dex.Flow[GetTicketInput]):
    """Run one KF5 query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("KF5 Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_ticket_step(
            step_type="CallProvider",
            step_input_type=GetTicketInput,
            annotations={"groupId": "kf5", "groupLabel": "KF5", "explanation": "Run KF5 getTicket."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "GetTicketKF5"

    def get_steps(self) -> dex.StepList[GetTicketInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
