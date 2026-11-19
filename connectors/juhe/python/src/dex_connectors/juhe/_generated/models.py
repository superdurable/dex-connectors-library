# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetCurrentWeatherInput identifies one supported Chinese city by name or Juhe city ID.
@dataclass(frozen=True, slots=True)
class GetCurrentWeatherInput:
    # City is the exact city name or ID from Juhe API 73's supported-city list.
    city: str = ''


setattr(GetCurrentWeatherInput, '__module__', 'dex_connectors.juhe.models')


# GetCurrentWeatherOutput contains only Juhe's current conditions; optional observations may be empty.
@dataclass(frozen=True, slots=True)
class GetCurrentWeatherOutput:
    # City is the provider's returned city name.
    city: str = ''
    # WeatherText is the current condition description.
    weather_text: str = _dataclass_field(default='', metadata={'dex_json_name': 'weatherText'})
    # WeatherCode is the condition ID from Juhe's weather-type list.
    weather_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'weatherCode'})
    # TemperatureCelsius is the optional current temperature in Celsius.
    temperature_celsius: str = _dataclass_field(default='', metadata={'dex_json_name': 'temperatureCelsius'})
    # HumidityPercent is the optional relative humidity percentage.
    humidity_percent: str = _dataclass_field(default='', metadata={'dex_json_name': 'humidityPercent'})
    # WindDirection is the optional wind direction text.
    wind_direction: str = _dataclass_field(default='', metadata={'dex_json_name': 'windDirection'})
    # WindPower is the optional wind power text.
    wind_power: str = _dataclass_field(default='', metadata={'dex_json_name': 'windPower'})
    # AirQualityIndex is the optional AQI text.
    air_quality_index: str = _dataclass_field(default='', metadata={'dex_json_name': 'airQualityIndex'})


setattr(GetCurrentWeatherOutput, '__module__', 'dex_connectors.juhe.models')
