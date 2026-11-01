# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class WeatherInput:
    """Typed WeatherInput value from the Go connector contract."""

    latitude: float = 0.0
    longitude: float = 0.0
    units: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(WeatherInput, '__module__', 'dex_connectors.openweathermap.models')


@dataclass(frozen=True, slots=True)
class CurrentWeatherOutput:
    """Typed CurrentWeatherOutput value from the Go connector contract."""

    city: str = ''
    temperature: float = 0.0
    description: str = ''


setattr(CurrentWeatherOutput, '__module__', 'dex_connectors.openweathermap.models')


@dataclass(frozen=True, slots=True)
class ForecastEntry:
    """Typed ForecastEntry value from the Go connector contract."""

    timestamp: int = 0
    temperature: float = 0.0


setattr(ForecastEntry, '__module__', 'dex_connectors.openweathermap.models')


@dataclass(frozen=True, slots=True)
class ForecastOutput:
    """Typed ForecastOutput value from the Go connector contract."""

    city: str = ''
    entries: tuple[ForecastEntry, ...] | None = None


setattr(ForecastOutput, '__module__', 'dex_connectors.openweathermap.models')
