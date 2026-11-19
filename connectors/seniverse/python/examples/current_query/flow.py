# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Seniverse provider data through a durable Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.seniverse import (
    Connection,
    GetCurrentWeatherInput,
    GetCurrentWeatherOutput,
    new_get_current_weather_step,
)

CONNECTION_NAME = "seniverse"
_RESULT = dex.Attribute("seniverse-python-query-result", GetCurrentWeatherOutput)


def map_to_operation_input(input: GetCurrentWeatherInput) -> GetCurrentWeatherInput:
    """Forward the provider Query input."""
    return input


# dex:group group-id:seniverse group-label:"Seniverse"
# dex:explanation text:"Start the Seniverse provider Query."
class _StartQuery(dex.Step[GetCurrentWeatherInput]):
    def get_step_type(self) -> str:
        return "StartSeniverse"

    def execute(self, context: dex.Context, input: GetCurrentWeatherInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallSeniverse"), input)


# dex:group group-id:seniverse group-label:"Seniverse"
# dex:explanation text:"Persist the Seniverse Query output and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[GetCurrentWeatherOutput]]):
    def get_step_type(self) -> str:
        return "CompleteSeniverse"

    def execute(self, context: dex.Context, input: QueryResult[GetCurrentWeatherOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetCurrentWeatherFlow(dex.Flow[GetCurrentWeatherInput]):
    """Read Seniverse through the static connection and persist its result."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Seniverse Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_current_weather_step(
            step_type="CallSeniverse",
            step_input_type=GetCurrentWeatherInput,
            annotations={
                "groupId": "seniverse",
                "groupLabel": "Seniverse",
                "explanation": "Read Seniverse provider data.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            fetched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Seniverse Flow identity."""
        return "SeniverseGetCurrentWeather"

    def get_steps(self) -> dex.StepList[GetCurrentWeatherInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable result Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
