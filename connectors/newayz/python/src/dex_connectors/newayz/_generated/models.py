# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetCurrentWeatherInput selects one six-digit Chinese city or district code.
@dataclass(frozen=True, slots=True)
class GetCurrentWeatherInput:
    # CityCode is a six-digit administrative code such as 420100 for Wuhan.
    city_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'cityCode'})


setattr(GetCurrentWeatherInput, '__module__', 'dex_connectors.newayz.models')


# GetCurrentWeatherOutput contains one WAYZ live weather observation.
@dataclass(frozen=True, slots=True)
class GetCurrentWeatherOutput:
    # Province is the province name.
    province: str = ''
    # City is the city name.
    city: str = ''
    # CityCode is the administrative code returned by WAYZ.
    city_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'cityCode'})
    # Weather is the current weather description.
    weather: str = ''
    # TemperatureCelsius is the observed Celsius temperature as a decimal string.
    temperature_celsius: str = _dataclass_field(default='', metadata={'dex_json_name': 'temperatureCelsius'})
    # WindDirection is the current wind direction.
    wind_direction: str = _dataclass_field(default='', metadata={'dex_json_name': 'windDirection'})
    # WindPower is the provider wind scale string.
    wind_power: str = _dataclass_field(default='', metadata={'dex_json_name': 'windPower'})
    # HumidityPercent is the relative humidity percent as a decimal string.
    humidity_percent: str = _dataclass_field(default='', metadata={'dex_json_name': 'humidityPercent'})
    # ReportTime is the provider observation timestamp string.
    report_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'reportTime'})


setattr(GetCurrentWeatherOutput, '__module__', 'dex_connectors.newayz.models')
