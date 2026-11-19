# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read pingpp through one durable provider Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.pingpp import (
    Connection,
    GetChargeStatusInput,
    GetChargeStatusOutput,
    new_get_charge_status_step,
)

CONNECTION_NAME = "pingpp"
_OUTCOME = dex.Attribute("pingpp-get_charge_status-outcome", GetChargeStatusOutput)


def map_to_operation_input(input: GetChargeStatusInput) -> GetChargeStatusInput:
    """Forward the typed Flow input to the provider Query."""
    return input


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Begin the pingpp read with the configured connection."
class _Start(dex.Step[GetChargeStatusInput]):
    def get_step_type(self) -> str:
        return "StartGetChargeStatus"

    def execute(self, context: dex.Context, input: GetChargeStatusInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetChargeStatusQuery"), input)


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _Complete(dex.Step[QueryResult[GetChargeStatusOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetChargeStatus"

    def execute(self, context: dex.Context, input: QueryResult[GetChargeStatusOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetChargeStatusFlow(dex.Flow[GetChargeStatusInput]):
    """Durably read pingpp with its static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("pingpp Flow requires its static connection")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_get_charge_status_step(
            step_type="GetChargeStatusQuery",
            step_input_type=GetChargeStatusInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "pingpp",
                "explanation": "Call pingpp getChargeStatus.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            fetched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "PingppGetChargeStatus"

    def get_steps(self) -> dex.StepList[GetChargeStatusInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
