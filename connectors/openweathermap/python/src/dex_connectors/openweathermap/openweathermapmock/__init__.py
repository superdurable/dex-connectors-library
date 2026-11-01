# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from .._generated.mock import Mock as Mock
from .._generated.mock import get_current_weather_found as get_current_weather_found
from .._generated.mock import get_current_weather_found_default as get_current_weather_default_answer
from .._generated.mock import get_current_weather_not_found as get_current_weather_not_found
from .._generated.mock import get_forecast_found as get_forecast_found
from .._generated.mock import get_forecast_found_default as get_forecast_default_answer

__all__ = [
    'Mock',
    'get_current_weather_found',
    'get_current_weather_not_found',
    'get_current_weather_default_answer',
    'get_forecast_found',
    'get_forecast_default_answer',
]
