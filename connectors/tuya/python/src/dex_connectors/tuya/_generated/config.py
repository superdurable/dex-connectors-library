# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    endpoint: str = "https://openapi.tuyacn.com"
    timeout_seconds: float = 10
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.tuya.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    client_id: SecretString
    client_secret: SecretString

setattr(Credentials, '__module__', 'dex_connectors.tuya.client')
