# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetCurrentWeatherInput selects a city ID, city name, or other documented Seniverse location.
@dataclass(frozen=True, slots=True)
class GetCurrentWeatherInput:
    # Location is a Seniverse city ID, Chinese city name, pinyin name, or latitude:longitude pair.
    location: str = ''


setattr(GetCurrentWeatherInput, '__module__', 'dex_connectors.seniverse.models')


# GetCurrentWeatherOutput contains fields available to free and paid Seniverse products.
@dataclass(frozen=True, slots=True)
class GetCurrentWeatherOutput:
    # LocationID is Seniverse's city ID.
    location_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'locationId'})
    # LocationName is Seniverse's localized city name.
    location_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'locationName'})
    # Country is the two-letter country code.
    country: str = ''
    # WeatherText is the localized current weather condition.
    weather_text: str = _dataclass_field(default='', metadata={'dex_json_name': 'weatherText'})
    # WeatherCode is Seniverse's weather condition code.
    weather_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'weatherCode'})
    # Temperature is the provider's numeric temperature string.
    temperature: str = ''
    # TemperatureUnit is c for Celsius or f for Fahrenheit.
    temperature_unit: str = _dataclass_field(default='', metadata={'dex_json_name': 'temperatureUnit'})
    # LastUpdate is the provider's RFC3339 local observation time.
    last_update: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastUpdate'})


setattr(GetCurrentWeatherOutput, '__module__', 'dex_connectors.seniverse.models')
