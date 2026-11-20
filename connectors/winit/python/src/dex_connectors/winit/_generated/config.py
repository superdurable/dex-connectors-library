# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    platform: str
    base_url: str = "https://openapi.winit.com.cn"
    timeout_seconds: float = 15
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.winit.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    app_key: SecretString
    token: SecretString
    client_id: SecretString
    client_secret: SecretString

setattr(Credentials, '__module__', 'dex_connectors.winit.client')
