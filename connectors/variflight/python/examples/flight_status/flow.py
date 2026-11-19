# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable VariFlight get flight status Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.variflight import (
    Connection,
    GetFlightStatusInput,
    GetFlightStatusOutput,
    new_get_flight_status_step,
)

CONNECTION_NAME = "variflight"
_RESULT = dex.Attribute("variflight-get-flight-status-result", GetFlightStatusOutput)


def map_to_operation_input(request: GetFlightStatusInput) -> GetFlightStatusInput:
    """Forward the typed request to the provider Query."""
    return request


# dex:group group-id:variflight group-label:"VariFlight"
# dex:explanation text:"Start the VariFlight get flight status request."
class _StartQuery(dex.Step[GetFlightStatusInput]):
    def get_step_type(self) -> str:
        return "StartVariFlightStatus"

    def execute(self, context: dex.Context, input: GetFlightStatusInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallVariFlightStatus"), input)


# dex:group group-id:variflight group-label:"VariFlight"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[GetFlightStatusOutput]]):
    def get_step_type(self) -> str:
        return "CompleteVariFlightStatus"

    def execute(self, context: dex.Context, input: QueryResult[GetFlightStatusOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class VariFlightStatusFlow(dex.Flow[GetFlightStatusInput]):
    """Run the get flight status Query with a static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("VariFlightStatusFlow requires the static VariFlight connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_flight_status_step(
            step_type="CallVariFlightStatus",
            step_input_type=GetFlightStatusInput,
            annotations={
                "groupId": "variflight",
                "groupLabel": "VariFlight",
                "explanation": "Get flight status with the configured VariFlight connection.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable get flight status Flow identity."""
        return "VariFlightStatus"

    def get_steps(self) -> dex.StepList[GetFlightStatusInput]:
        """Register the application and connector Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the bounded get flight status result."""
        return dex.PersistenceSchema.of(_RESULT)
