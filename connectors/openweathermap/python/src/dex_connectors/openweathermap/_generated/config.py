# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated connection settings and credential values."""

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:
    """OpenWeatherMap connection settings from the manifest."""

    base_url: str = "https://api.openweathermap.org"
    timeout_seconds: float = 15
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.openweathermap.client')


@dataclass(frozen=True, slots=True)
class Credentials:
    """OpenWeatherMap provider credentials resolved per call."""

    api_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.openweathermap.client')
