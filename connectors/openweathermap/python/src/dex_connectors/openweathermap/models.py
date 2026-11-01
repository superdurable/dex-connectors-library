# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass

from ._generated.models import (
    CurrentWeatherOutput as CurrentWeatherOutput,
)
from ._generated.models import (
    ForecastEntry as ForecastEntry,
)
from ._generated.models import (
    WeatherInput as WeatherInput,
)


@dataclass(frozen=True, slots=True)
class ForecastOutput:
    """City name and forecast points in provider order."""

    city: str = ""
    entries: tuple[ForecastEntry, ...] = ()
