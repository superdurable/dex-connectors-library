# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Caiyun provider data through a durable Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.caiyun import (
    Connection,
    GetCurrentWeatherInput,
    GetCurrentWeatherOutput,
    new_get_current_weather_step,
)

CONNECTION_NAME = "caiyun"
_RESULT = dex.Attribute("caiyun-current-weather", GetCurrentWeatherOutput)


def map_to_operation_input(input: GetCurrentWeatherInput) -> GetCurrentWeatherInput:
    """Forward the nonsecret provider Query input."""
    return input


# dex:group group-id:current-weather group-label:"Caiyun"
# dex:explanation text:"Start the Caiyun provider Query."
class _StartQuery(dex.Step[GetCurrentWeatherInput]):
    def get_step_type(self) -> str:
        return "StartCaiyunCurrentWeather"

    def execute(self, context: dex.Context, input: GetCurrentWeatherInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ReadCaiyunCurrentWeather"), input)


# dex:group group-id:current-weather group-label:"Caiyun"
# dex:explanation text:"Persist the bounded Caiyun result and complete."
class _CompleteQuery(dex.Step[QueryResult[GetCurrentWeatherOutput]]):
    def get_step_type(self) -> str:
        return "CompleteCaiyunCurrentWeather"

    def execute(self, context: dex.Context, input: QueryResult[GetCurrentWeatherOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CurrentWeatherFlow(dex.Flow[GetCurrentWeatherInput]):
    """Run the Caiyun Query with the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Caiyun Flow requires the static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_get_current_weather_step(
            step_type="ReadCaiyunCurrentWeather",
            step_input_type=GetCurrentWeatherInput,
            annotations={
                "groupId": "current-weather",
                "groupLabel": "Caiyun",
                "explanation": "Read the bounded provider result through Caiyun.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            fetched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "CaiyunCurrentWeatherPython"

    def get_steps(self) -> dex.StepList[GetCurrentWeatherInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the bounded result Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
