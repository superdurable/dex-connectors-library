# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated manifest-backed provider outcomes and scripts."""

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CurrentWeatherOutput, ForecastEntry, ForecastOutput, WeatherInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_CURRENT_WEATHER_DEFINITION, GET_FORECAST_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_current_weather_found(value: CurrentWeatherOutput, failure: Failure | None = None) -> Case[CurrentWeatherOutput]:
    """Script the found branch."""
    return Case.branch_result(FOUND, value, failure)

setattr(get_current_weather_found, '__module__', "dex_connectors.openweathermap.openweathermapmock")

def get_current_weather_not_found(value: CurrentWeatherOutput, failure: Failure | None = None) -> Case[CurrentWeatherOutput]:
    """Script the notFound branch."""
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_current_weather_not_found, '__module__', "dex_connectors.openweathermap.openweathermapmock")

def get_current_weather_provider_rejected(value: CurrentWeatherOutput, failure: Failure | None = None) -> Case[CurrentWeatherOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_current_weather_provider_rejected, '__module__', "dex_connectors.openweathermap.openweathermapmock")

def get_current_weather_invalid_response(value: CurrentWeatherOutput, failure: Failure | None = None) -> Case[CurrentWeatherOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_current_weather_invalid_response, '__module__', "dex_connectors.openweathermap.openweathermapmock")

def get_current_weather_defect(value: CurrentWeatherOutput, failure: Failure | None = None) -> Case[CurrentWeatherOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(get_current_weather_defect, '__module__', "dex_connectors.openweathermap.openweathermapmock")

def get_current_weather_retry(failure: Failure, after_seconds: float = 0) -> Case[CurrentWeatherOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(get_current_weather_retry, '__module__', "dex_connectors.openweathermap.openweathermapmock")

def get_current_weather_found_default() -> Case[CurrentWeatherOutput]:
    """Return the manifest foundDefault outcome."""
    return Case.branch_result(FOUND, CurrentWeatherOutput(city='Meridian', temperature=21.5, description='clear sky'))

setattr(get_current_weather_found_default, '__module__', "dex_connectors.openweathermap.openweathermapmock")

def get_forecast_found(value: ForecastOutput, failure: Failure | None = None) -> Case[ForecastOutput]:
    """Script the found branch."""
    return Case.branch_result(FOUND, value, failure)

setattr(get_forecast_found, '__module__', "dex_connectors.openweathermap.openweathermapmock")

def get_forecast_not_found(value: ForecastOutput, failure: Failure | None = None) -> Case[ForecastOutput]:
    """Script the notFound branch."""
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_forecast_not_found, '__module__', "dex_connectors.openweathermap.openweathermapmock")

def get_forecast_provider_rejected(value: ForecastOutput, failure: Failure | None = None) -> Case[ForecastOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_forecast_provider_rejected, '__module__', "dex_connectors.openweathermap.openweathermapmock")

def get_forecast_invalid_response(value: ForecastOutput, failure: Failure | None = None) -> Case[ForecastOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_forecast_invalid_response, '__module__', "dex_connectors.openweathermap.openweathermapmock")

def get_forecast_defect(value: ForecastOutput, failure: Failure | None = None) -> Case[ForecastOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(get_forecast_defect, '__module__', "dex_connectors.openweathermap.openweathermapmock")

def get_forecast_retry(failure: Failure, after_seconds: float = 0) -> Case[ForecastOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(get_forecast_retry, '__module__', "dex_connectors.openweathermap.openweathermapmock")

def get_forecast_found_default() -> Case[ForecastOutput]:
    """Return the manifest foundDefault outcome."""
    return Case.branch_result(FOUND, ForecastOutput(city='Meridian', entries=(ForecastEntry(timestamp=1793210400, temperature=20.1),)))

setattr(get_forecast_found_default, '__module__', "dex_connectors.openweathermap.openweathermapmock")


class Mock:
    """Script manifest outcomes for this connector."""

    def __init__(self, connection_name: str = "openweathermap") -> None:
        self.get_current_weather_script: Query[WeatherInput, CurrentWeatherOutput] = Query(GET_CURRENT_WEATHER_DEFINITION)
        self.get_current_weather_script.default(get_current_weather_found_default())
        self.get_forecast_script: Query[WeatherInput, ForecastOutput] = Query(GET_FORECAST_DEFINITION)
        self.get_forecast_script.default(get_forecast_found_default())
        self._connection = new_connection_with_operations(
            get_current_weather=self.get_current_weather_script,
            get_forecast=self.get_forecast_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock connection for Dex Step registration."""
        return self._connection

setattr(Mock, '__module__', "dex_connectors.openweathermap.openweathermapmock")

__all__ = [
    "Mock",
    "get_current_weather_found",
    "get_current_weather_not_found",
    "get_current_weather_provider_rejected",
    "get_current_weather_invalid_response",
    "get_current_weather_defect",
    "get_current_weather_retry",
    "get_current_weather_found_default",
    "get_forecast_found",
    "get_forecast_not_found",
    "get_forecast_provider_rejected",
    "get_forecast_invalid_response",
    "get_forecast_defect",
    "get_forecast_retry",
    "get_forecast_found_default",
]
