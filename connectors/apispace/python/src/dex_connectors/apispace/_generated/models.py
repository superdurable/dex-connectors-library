# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetCurrentWeatherInput selects one city by the provider's documented area code.
@dataclass(frozen=True, slots=True)
class GetCurrentWeatherInput:
    # AreaCode is a city ID from the APISpace weather product's city list.
    area_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'areaCode'})


setattr(GetCurrentWeatherInput, '__module__', 'dex_connectors.apispace.models')


# CurrentWeather contains selected public observations for one city.
@dataclass(frozen=True, slots=True)
class CurrentWeather:
    # City is the provider's city display name.
    city: str = ''
    # Condition is the current weather description.
    condition: str = ''
    # TemperatureCelsius is the current air temperature in degrees Celsius.
    temperature_celsius: float = _dataclass_field(default=0.0, metadata={'dex_json_name': 'temperatureCelsius'})
    # HumidityPercent is the relative humidity from 0 to 100 percent.
    humidity_percent: int = _dataclass_field(default=0, metadata={'dex_json_name': 'humidityPercent'})
    # UpdatedAtBeijing is the provider update time in Beijing local time.
    updated_at_beijing: str = _dataclass_field(default='', metadata={'dex_json_name': 'updatedAtBeijing'})


setattr(CurrentWeather, '__module__', 'dex_connectors.apispace.models')
