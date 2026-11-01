# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    CurrentWeatherOutput as CurrentWeatherOutput,
)
from ._generated.models import (
    ForecastEntry as ForecastEntry,
)
from ._generated.models import ForecastOutput as ForecastOutput
from ._generated.models import (
    WeatherInput as WeatherInput,
)

ForecastOutput.__doc__ = "City name and forecast points in provider order."

WeatherInput.__doc__ = (
    "Coordinates in decimal degrees and units: standard (kelvin, default), metric, or imperial."
)
CurrentWeatherOutput.__doc__ = (
    "City name, temperature in the requested units, and the first weather description."
)
ForecastEntry.__doc__ = "Forecast point at a Unix timestamp in seconds."
