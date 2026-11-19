# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read current weather for one Chinese area code or coordinate."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.geovisearth import (
    Connection,
    GetCurrentWeatherInput,
    GetCurrentWeatherOutput,
    new_get_current_weather_step,
)

CONNECTION_NAME = "geovisearth"
_OUTCOME = dex.Attribute("geovisearth-current-weather-outcome", GetCurrentWeatherOutput)


def map_to_operation_input(input: GetCurrentWeatherInput) -> GetCurrentWeatherInput:
    """Pass the requested value to the provider Query."""
    return input


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Start the provider query."
class _StartQuery(dex.Step[GetCurrentWeatherInput]):
    def get_step_type(self) -> str:
        return "StartCurrentWeather"

    def execute(self, context: dex.Context, input: GetCurrentWeatherInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetCurrentWeatherQuery"), input)


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[GetCurrentWeatherOutput]]):
    def get_step_type(self) -> str:
        return "CompleteCurrentWeather"

    def execute(self, context: dex.Context, input: QueryResult[GetCurrentWeatherOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class GEOVISEarthCurrentWeatherFlow(dex.Flow[GetCurrentWeatherInput]):
    """Run geovisearth's Query with its static project connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("example requires the static geovisearth connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_get_current_weather_step(
            step_type="GetCurrentWeatherQuery",
            step_input_type=GetCurrentWeatherInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "geovisearth",
                "explanation": "Call geovisearth GetCurrentWeather.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            fetched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "GEOVISEarthCurrentWeatherFlow"

    def get_steps(self) -> dex.StepList[GetCurrentWeatherInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable result Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
