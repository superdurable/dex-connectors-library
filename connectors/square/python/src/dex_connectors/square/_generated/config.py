# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated connection settings and credential values."""

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:
    """Square connection settings from the manifest."""

    base_url: str = "https://connect.squareup.com/v2"
    timeout_seconds: float = 15
    max_response_bytes: int = 2097152

setattr(Config, '__module__', 'dex_connectors.square.client')


@dataclass(frozen=True, slots=True)
class Credentials:
    """Square provider credentials resolved per call."""

    access_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.square.client')
