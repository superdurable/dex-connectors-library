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


# GetCurrentWeatherInput locates the weather sample in decimal degrees.
@dataclass(frozen=True, slots=True)
class GetCurrentWeatherInput:
    # Latitude is geographic latitude in decimal degrees, from -90 to 90.
    latitude: float = 0.0
    # Longitude is geographic longitude in decimal degrees, from -180 to 180.
    longitude: float = 0.0


setattr(GetCurrentWeatherInput, '__module__', 'dex_connectors.qweather.models')


# GetCurrentWeatherOutput contains current condition, temperature, time, and provider attribution.
@dataclass(frozen=True, slots=True)
class GetCurrentWeatherOutput:
    # ConditionText is the localized human-readable weather condition.
    condition_text: str = _dataclass_field(default='', metadata={'dex_json_name': 'conditionText'})
    # ConditionCode is the QWeather condition code.
    condition_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'conditionCode'})
    # TemperatureValue is the measured temperature in TemperatureUnit.
    temperature_value: float = _dataclass_field(default=0.0, metadata={'dex_json_name': 'temperatureValue'})
    # TemperatureUnit is the provider's temperature unit, usually °C or °F.
    temperature_unit: str = _dataclass_field(default='', metadata={'dex_json_name': 'temperatureUnit'})
    # ValidTime is the ISO 8601 time represented by this observation.
    valid_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'validTime'})
    # Attributions must accompany displayed weather data under provider guidance.
    attributions: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'attributions', _replace_nil_slices(self.attributions, 1))


GetCurrentWeatherOutput.__annotations__['attributions'] = tuple[str, ...] | None
GetCurrentWeatherOutput.__dataclass_fields__['attributions'].type = tuple[str, ...] | None
setattr(GetCurrentWeatherOutput, '__module__', 'dex_connectors.qweather.models')
