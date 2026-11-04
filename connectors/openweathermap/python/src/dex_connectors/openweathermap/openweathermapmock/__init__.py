# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Case as _Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import get_current_weather_found as get_current_weather_found
from .._generated.mock import get_current_weather_found_default as _get_current_weather_found_default
from .._generated.mock import get_current_weather_not_found as get_current_weather_not_found
from .._generated.mock import get_forecast_found as get_forecast_found
from .._generated.mock import get_forecast_found_default as _get_forecast_found_default
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import FOUND as FOUND
from ..client import GET_CURRENT_WEATHER_DEFINITION as GET_CURRENT_WEATHER_DEFINITION
from ..client import GET_FORECAST_DEFINITION as GET_FORECAST_DEFINITION
from ..client import NOT_FOUND as NOT_FOUND
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import CurrentWeatherOutput as CurrentWeatherOutput
from ..models import CurrentWeatherOutput as _CurrentWeatherOutput
from ..models import ForecastEntry as ForecastEntry
from ..models import ForecastOutput as ForecastOutput
from ..models import ForecastOutput as _ForecastOutput
from ..models import WeatherInput as WeatherInput


def get_current_weather_default_answer() -> _Case[_CurrentWeatherOutput]:
    """Return the manifest's getCurrentWeather default answer."""
    return _get_current_weather_found_default()


def get_forecast_default_answer() -> _Case[_ForecastOutput]:
    """Return the manifest's getForecast default answer."""
    return _get_forecast_found_default()


__all__ = [
    "Mock",
    "get_current_weather_found",
    "get_current_weather_not_found",
    "get_current_weather_default_answer",
    "get_forecast_found",
    "get_forecast_default_answer",
]

Mock.__doc__ = "Script OpenWeatherMap Queries, defaulting to their manifest answers."

get_current_weather_found.__doc__ = "Script the successful found route."
get_current_weather_not_found.__doc__ = "Script coordinates without weather data."
get_forecast_found.__doc__ = "Script the successful found route."
