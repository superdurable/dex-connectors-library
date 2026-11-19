# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetCurrentWeatherInput, GetCurrentWeatherOutput
from .definitions import CONNECTOR_ID, DEFECT, FETCHED, GET_CURRENT_WEATHER_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_current_weather_fetched(value: GetCurrentWeatherOutput, failure: Failure | None = None) -> Case[GetCurrentWeatherOutput]:
    return Case.branch_result(FETCHED, value, failure)

setattr(get_current_weather_fetched, '__module__', "dex_connectors.seniverse.seniversemock")

def get_current_weather_provider_rejected(value: GetCurrentWeatherOutput, failure: Failure | None = None) -> Case[GetCurrentWeatherOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_current_weather_provider_rejected, '__module__', "dex_connectors.seniverse.seniversemock")

def get_current_weather_invalid_response(value: GetCurrentWeatherOutput, failure: Failure | None = None) -> Case[GetCurrentWeatherOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_current_weather_invalid_response, '__module__', "dex_connectors.seniverse.seniversemock")

def get_current_weather_defect(value: GetCurrentWeatherOutput, failure: Failure | None = None) -> Case[GetCurrentWeatherOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_current_weather_defect, '__module__', "dex_connectors.seniverse.seniversemock")

def get_current_weather_retry(failure: Failure, after_seconds: float = 0) -> Case[GetCurrentWeatherOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_current_weather_retry, '__module__', "dex_connectors.seniverse.seniversemock")

def get_current_weather_beijing_current_weather() -> Case[GetCurrentWeatherOutput]:
    return Case.branch_result(FETCHED, GetCurrentWeatherOutput(location_id='WX4FBXXFKE4F', location_name='北京', country='CN', weather_text='晴', weather_code='0', temperature='20', temperature_unit='c', last_update='2026-01-01T12:00:00+08:00'))

setattr(get_current_weather_beijing_current_weather, '__module__', "dex_connectors.seniverse.seniversemock")


class Mock:

    def __init__(self, connection_name: str = "seniverse") -> None:
        self.get_current_weather_script: Query[GetCurrentWeatherInput, GetCurrentWeatherOutput] = Query(GET_CURRENT_WEATHER_DEFINITION)
        self.get_current_weather_script.default(get_current_weather_beijing_current_weather())
        self._connection = new_connection_with_operations(
            get_current_weather=self.get_current_weather_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.seniverse.seniversemock")

__all__ = [
    "Mock",
    "get_current_weather_fetched",
    "get_current_weather_provider_rejected",
    "get_current_weather_invalid_response",
    "get_current_weather_defect",
    "get_current_weather_retry",
    "get_current_weather_beijing_current_weather",
]
