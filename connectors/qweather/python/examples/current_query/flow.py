# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read QWeather provider data through a durable Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.qweather import (
    Connection,
    GetCurrentWeatherInput,
    GetCurrentWeatherOutput,
    new_get_current_weather_step,
)

CONNECTION_NAME = "qweather"
_RESULT = dex.Attribute("qweather-python-query-result", GetCurrentWeatherOutput)


def map_to_operation_input(input: GetCurrentWeatherInput) -> GetCurrentWeatherInput:
    """Forward the provider Query input."""
    return input


# dex:group group-id:qweather group-label:"QWeather"
# dex:explanation text:"Start the QWeather provider Query."
class _StartQuery(dex.Step[GetCurrentWeatherInput]):
    def get_step_type(self) -> str:
        return "StartQWeather"

    def execute(self, context: dex.Context, input: GetCurrentWeatherInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallQWeather"), input)


# dex:group group-id:qweather group-label:"QWeather"
# dex:explanation text:"Persist the QWeather Query output and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[GetCurrentWeatherOutput]]):
    def get_step_type(self) -> str:
        return "CompleteQWeather"

    def execute(self, context: dex.Context, input: QueryResult[GetCurrentWeatherOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetCurrentWeatherFlow(dex.Flow[GetCurrentWeatherInput]):
    """Read QWeather through the static connection and persist its result."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("QWeather Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_current_weather_step(
            step_type="CallQWeather",
            step_input_type=GetCurrentWeatherInput,
            annotations={
                "groupId": "qweather",
                "groupLabel": "QWeather",
                "explanation": "Read QWeather provider data.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable QWeather Flow identity."""
        return "QWeatherGetCurrentWeather"

    def get_steps(self) -> dex.StepList[GetCurrentWeatherInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable result Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
