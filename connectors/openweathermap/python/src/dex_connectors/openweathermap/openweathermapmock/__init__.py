# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed OpenWeatherMap scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import CONNECTOR_ID, FOUND, GET_CURRENT_WEATHER_DEFINITION, GET_FORECAST_DEFINITION, NOT_FOUND
from ..connection import Connection, new_connection_with_operations
from ..models import CurrentWeatherOutput, ForecastEntry, ForecastOutput, WeatherInput


def get_current_weather_found(
    value: CurrentWeatherOutput, failure: Failure | None = None
) -> Case[CurrentWeatherOutput]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_current_weather_not_found(
    value: CurrentWeatherOutput, failure: Failure | None = None
) -> Case[CurrentWeatherOutput]:
    """Script coordinates without weather data."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_current_weather_default_answer() -> Case[CurrentWeatherOutput]:
    """Return the manifest's getCurrentWeather default answer."""
    return get_current_weather_found(CurrentWeatherOutput("Meridian", 21.5, "clear sky"))


def get_forecast_found(value: ForecastOutput, failure: Failure | None = None) -> Case[ForecastOutput]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_forecast_default_answer() -> Case[ForecastOutput]:
    """Return the manifest's getForecast default answer."""
    return get_forecast_found(ForecastOutput("Meridian", (ForecastEntry(1793210400, 20.1),)))


class Mock:
    """Script OpenWeatherMap Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "openweathermap") -> None:
        self.get_current_weather_script: Query[WeatherInput, CurrentWeatherOutput] = Query(
            GET_CURRENT_WEATHER_DEFINITION
        )
        self.get_current_weather_script.default(get_current_weather_default_answer())
        self.get_forecast_script: Query[WeatherInput, ForecastOutput] = Query(GET_FORECAST_DEFINITION)
        self.get_forecast_script.default(get_forecast_default_answer())
        self._connection = new_connection_with_operations(
            self.get_current_weather_script,
            self.get_forecast_script,
            ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "get_current_weather_found",
    "get_current_weather_not_found",
    "get_current_weather_default_answer",
    "get_forecast_found",
    "get_forecast_default_answer",
]
