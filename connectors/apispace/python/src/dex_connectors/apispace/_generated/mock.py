# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CurrentWeather, GetCurrentWeatherInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_CURRENT_WEATHER_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_current_weather_found(value: CurrentWeather, failure: Failure | None = None) -> Case[CurrentWeather]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_current_weather_found, '__module__', "dex_connectors.apispace.apispacemock")

def get_current_weather_provider_rejected(value: CurrentWeather, failure: Failure | None = None) -> Case[CurrentWeather]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_current_weather_provider_rejected, '__module__', "dex_connectors.apispace.apispacemock")

def get_current_weather_invalid_response(value: CurrentWeather, failure: Failure | None = None) -> Case[CurrentWeather]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_current_weather_invalid_response, '__module__', "dex_connectors.apispace.apispacemock")

def get_current_weather_defect(value: CurrentWeather, failure: Failure | None = None) -> Case[CurrentWeather]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_current_weather_defect, '__module__', "dex_connectors.apispace.apispacemock")

def get_current_weather_retry(failure: Failure, after_seconds: float = 0) -> Case[CurrentWeather]:
    return Case.retry(failure, after_seconds)

setattr(get_current_weather_retry, '__module__', "dex_connectors.apispace.apispacemock")

def get_current_weather_current_weather() -> Case[CurrentWeather]:
    return Case.branch_result(FOUND, CurrentWeather(city='Beijing', condition='Cloudy', temperature_celsius=18.5, humidity_percent=38, updated_at_beijing='2026-11-13 12:00:00'))

setattr(get_current_weather_current_weather, '__module__', "dex_connectors.apispace.apispacemock")


class Mock:

    def __init__(self, connection_name: str = "apispace") -> None:
        self.get_current_weather_script: Query[GetCurrentWeatherInput, CurrentWeather] = Query(GET_CURRENT_WEATHER_DEFINITION)
        self.get_current_weather_script.default(get_current_weather_current_weather())
        self._connection = new_connection_with_operations(
            get_current_weather=self.get_current_weather_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.apispace.apispacemock")

__all__ = [
    "Mock",
    "get_current_weather_found",
    "get_current_weather_provider_rejected",
    "get_current_weather_invalid_response",
    "get_current_weather_defect",
    "get_current_weather_retry",
    "get_current_weather_current_weather",
]
