# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable APISpace current-weather Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.apispace import (
    Connection,
    CurrentWeather,
    GetCurrentWeatherInput,
    new_get_current_weather_step,
)

CONNECTION_NAME = "apispace"
_RESULT = dex.Attribute("apispace-current-weather", CurrentWeather)


def map_to_operation_input(request: GetCurrentWeatherInput) -> GetCurrentWeatherInput:
    """Forward the documented provider area code to APISpace."""
    return request


# dex:group group-id:apispace group-label:"APISpace"
# dex:explanation text:"Start the APISpace current-weather request."
class _StartWeather(dex.Step[GetCurrentWeatherInput]):
    def get_step_type(self) -> str:
        return "StartAPISpaceCurrentWeather"

    def execute(self, context: dex.Context, input: GetCurrentWeatherInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallAPISpace"), input)


# dex:group group-id:apispace group-label:"APISpace"
# dex:explanation text:"Persist current weather and complete the Flow."
class _CompleteWeather(dex.Step[QueryResult[CurrentWeather]]):
    def get_step_type(self) -> str:
        return "CompleteAPISpaceCurrentWeather"

    def execute(self, context: dex.Context, input: QueryResult[CurrentWeather]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CurrentWeatherFlow(dex.Flow[GetCurrentWeatherInput]):
    """Read one city's current weather through the static APISpace connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("CurrentWeatherFlow requires the static APISpace connection")
        self._start = _StartWeather()
        self._complete = _CompleteWeather()
        self._call = new_get_current_weather_step(
            step_type="CallAPISpace",
            step_input_type=GetCurrentWeatherInput,
            annotations={
                "groupId": "apispace",
                "groupLabel": "APISpace",
                "explanation": "Read current conditions for one provider area code.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable current-weather Flow identity."""
        return "APISpaceCurrentWeather"

    def get_steps(self) -> dex.StepList[GetCurrentWeatherInput]:
        """Register the weather Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the current-weather result."""
        return dex.PersistenceSchema.of(_RESULT)
