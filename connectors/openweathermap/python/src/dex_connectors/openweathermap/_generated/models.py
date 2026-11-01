# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# WeatherInput selects coordinates and temperature units.
@dataclass(frozen=True, slots=True)
class WeatherInput:
    # Latitude is degrees north, from -90 through 90.
    latitude: float = 0.0
    # Longitude is degrees east, from -180 through 180.
    longitude: float = 0.0
    # Units is standard, metric, or imperial; blank uses standard.
    units: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(WeatherInput, '__module__', 'dex_connectors.openweathermap.models')


# CurrentWeatherOutput is the current temperature and condition at a location.
@dataclass(frozen=True, slots=True)
class CurrentWeatherOutput:
    # City is the provider's location name.
    city: str = ''
    # Temperature uses the requested units.
    temperature: float = 0.0
    # Description is the provider's weather condition text.
    description: str = ''


setattr(CurrentWeatherOutput, '__module__', 'dex_connectors.openweathermap.models')


# ForecastEntry is one three-hour forecast point.
@dataclass(frozen=True, slots=True)
class ForecastEntry:
    # Timestamp is the forecast's UTC Unix time in seconds.
    timestamp: int = 0
    # Temperature uses the requested units.
    temperature: float = 0.0


setattr(ForecastEntry, '__module__', 'dex_connectors.openweathermap.models')


# ForecastOutput contains one five-day forecast page.
@dataclass(frozen=True, slots=True)
class ForecastOutput:
    # City is the provider's location name.
    city: str = ''
    # Entries are the forecast's three-hour points.
    entries: tuple[ForecastEntry, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'entries', _replace_nil_slices(self.entries, 1))


ForecastOutput.__annotations__['entries'] = tuple[ForecastEntry, ...] | None
ForecastOutput.__dataclass_fields__['entries'].type = tuple[ForecastEntry, ...] | None
setattr(ForecastOutput, '__module__', 'dex_connectors.openweathermap.models')
