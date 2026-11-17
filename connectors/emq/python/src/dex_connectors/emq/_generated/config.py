# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    platform_endpoint: str
    timeout_seconds: float = 10
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.emq.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    platform_api_key: SecretString
    platform_api_secret: SecretString

setattr(Credentials, '__module__', 'dex_connectors.emq.client')
