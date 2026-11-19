# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://iot-api.heclouds.com"
    timeout_seconds: float = 15
    max_response_bytes: int = 65536

setattr(Config, '__module__', 'dex_connectors.onenet.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    user_id: str
    access_key: SecretString
    product_id: str

setattr(Credentials, '__module__', 'dex_connectors.onenet.client')
