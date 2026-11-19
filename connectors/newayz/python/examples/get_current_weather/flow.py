# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Newayz getCurrentWeather Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.newayz import (
    Connection,
    GetCurrentWeatherInput,
    GetCurrentWeatherOutput,
    new_get_current_weather_step,
)

CONNECTION_NAME = "newayz"
_RESULT = dex.Attribute("newayz-result", GetCurrentWeatherOutput)


def map_to_operation_input(request: GetCurrentWeatherInput) -> GetCurrentWeatherInput:
    """Forward the provider request."""
    return request


# dex:group group-id:newayz group-label:"Newayz"
# dex:explanation text:"Start the Newayz query."
class _StartQuery(dex.Step[GetCurrentWeatherInput]):
    def execute(self, context: dex.Context, input: GetCurrentWeatherInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:newayz group-label:"Newayz"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[GetCurrentWeatherOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[GetCurrentWeatherOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetCurrentWeatherFlow(dex.Flow[GetCurrentWeatherInput]):
    """Run one Newayz query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Newayz Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_current_weather_step(
            step_type="CallProvider",
            step_input_type=GetCurrentWeatherInput,
            annotations={
                "groupId": "newayz",
                "groupLabel": "Newayz",
                "explanation": "Run Newayz getCurrentWeather.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            fetched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "GetCurrentWeatherNewayz"

    def get_steps(self) -> dex.StepList[GetCurrentWeatherInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
