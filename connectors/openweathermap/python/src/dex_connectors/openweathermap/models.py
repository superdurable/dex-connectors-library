# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed OpenWeatherMap coordinates, current weather, and forecasts."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class WeatherInput:
    """Coordinates in decimal degrees and units: standard (kelvin, default), metric, or imperial."""

    latitude: float = 0.0
    longitude: float = 0.0
    units: str = field(default="", metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class CurrentWeatherOutput:
    """City name, temperature in the requested units, and the first weather description."""

    city: str = ""
    temperature: float = 0.0
    description: str = ""


@dataclass(frozen=True, slots=True)
class ForecastEntry:
    """Forecast point at a Unix timestamp in seconds."""

    timestamp: int = 0
    temperature: float = 0.0


@dataclass(frozen=True, slots=True)
class ForecastOutput:
    """City name and forecast points in provider order."""

    city: str = ""
    entries: tuple[ForecastEntry, ...] = ()
