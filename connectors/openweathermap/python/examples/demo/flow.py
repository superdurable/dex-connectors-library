# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable OpenWeatherMap demo Flow using the getCurrentWeather Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.openweathermap import (
    Connection,
    CurrentWeatherOutput,
    WeatherInput,
    new_get_current_weather_step,
)

FLOW_TYPE = "OpenWeatherMapDemo"
CONNECTION_NAME = "openweathermap"
_OPERATION_STEP_TYPE = "CallGetCurrentWeather"
_OUTCOME = dex.Attribute("openweathermap-demo-outcome", CurrentWeatherOutput)


def map_to_operation_input(input: WeatherInput) -> WeatherInput:
    """Read the requested coordinate unchanged."""
    return input


# dex:group group-id:demo group-label:"Demo"
# dex:explanation text:"Start the weather lookup with the requested coordinate."
class _StartRequest(dex.Step[WeatherInput]):
    def get_step_type(self) -> str:
        return "StartGetCurrentWeather"

    def execute(self, context: dex.Context, input: WeatherInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_OPERATION_STEP_TYPE), input)


# dex:group group-id:demo group-label:"Demo"
# dex:explanation text:"Record the current weather and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[CurrentWeatherOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetCurrentWeather"

    def execute(self, context: dex.Context, input: QueryResult[CurrentWeatherOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class DemoFlow(dex.Flow[WeatherInput]):
    """Read current weather through the static openweathermap connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("demo requires the static openweathermap connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_get_current_weather_step(
            step_type=_OPERATION_STEP_TYPE,
            step_input_type=WeatherInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "OpenWeatherMap",
                "explanation": "Call the provider's getCurrentWeather operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[WeatherInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the current weather outcome."""
        return dex.PersistenceSchema.of(_OUTCOME)
