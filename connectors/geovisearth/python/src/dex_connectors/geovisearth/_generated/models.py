# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetCurrentWeatherInput selects an official area code or longitude,latitude coordinate.
@dataclass(frozen=True, slots=True)
class GetCurrentWeatherInput:
    # Location is a GEOVIS Earth area code such as WTX_CH101300201, or longitude,latitude in decimal degrees.
    location: str = ''


setattr(GetCurrentWeatherInput, '__module__', 'dex_connectors.geovisearth.models')


# GetCurrentWeatherOutput contains the basic current-weather observation.
@dataclass(frozen=True, slots=True)
class GetCurrentWeatherOutput:
    # AreaCode is the area code, blank for coordinate lookups.
    area_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'areaCode'})
    # Weather is the Chinese weather condition description.
    weather: str = ''
    # WeatherCode is the provider's weather phenomenon code.
    weather_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'weatherCode'})
    # TemperatureCelsius is air temperature in degrees Celsius.
    temperature_celsius: float = _dataclass_field(default=0.0, metadata={'dex_json_name': 'temperatureCelsius'})
    # RelativeHumidityPercent is relative humidity from 0 to 100 percent.
    relative_humidity_percent: int = _dataclass_field(default=0, metadata={'dex_json_name': 'relativeHumidityPercent'})
    # WindDirection is the human-readable wind direction.
    wind_direction: str = _dataclass_field(default='', metadata={'dex_json_name': 'windDirection'})
    # WindForce is the human-readable wind-force level.
    wind_force: str = _dataclass_field(default='', metadata={'dex_json_name': 'windForce'})
    # SurfacePressureHpa is surface pressure in hectopascals.
    surface_pressure_hpa: float = _dataclass_field(default=0.0, metadata={'dex_json_name': 'surfacePressureHpa'})
    # ObservedAt is the provider's local observation timestamp formatted yyyyMMddHHmmss.
    observed_at: str = _dataclass_field(default='', metadata={'dex_json_name': 'observedAt'})


setattr(GetCurrentWeatherOutput, '__module__', 'dex_connectors.geovisearth.models')
