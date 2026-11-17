# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetCurrentWeatherInput locates one observation; mainland China uses GCJ-02 coordinates, elsewhere WGS84.
@dataclass(frozen=True, slots=True)
class GetCurrentWeatherInput:
    # Longitude is degrees east from -180 through 180.
    longitude: float = 0.0
    # Latitude is degrees north from -90 through 90.
    latitude: float = 0.0


setattr(GetCurrentWeatherInput, '__module__', 'dex_connectors.caiyun.models')


# GetCurrentWeatherOutput contains metric weather values without credentials or raw provider data.
@dataclass(frozen=True, slots=True)
class GetCurrentWeatherOutput:
    # TemperatureCelsius is air temperature at two meters.
    temperature_celsius: float = _dataclass_field(default=0.0, metadata={'dex_json_name': 'temperatureCelsius'})
    # RelativeHumidity is a fraction from zero to one.
    relative_humidity: float = _dataclass_field(default=0.0, metadata={'dex_json_name': 'relativeHumidity'})
    # SkyCondition is Caiyun's weather condition code.
    sky_condition: str = _dataclass_field(default='', metadata={'dex_json_name': 'skyCondition'})
    # WindSpeedMetersPerSecond is wind speed at ten meters.
    wind_speed_meters_per_second: float = _dataclass_field(default=0.0, metadata={'dex_json_name': 'windSpeedMetersPerSecond'})


setattr(GetCurrentWeatherOutput, '__module__', 'dex_connectors.caiyun.models')
