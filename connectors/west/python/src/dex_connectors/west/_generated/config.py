# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://api.west.cn"
    timeout_seconds: float = 10
    max_response_bytes: int = 2097152

setattr(Config, '__module__', 'dex_connectors.west.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    username: str
    api_password: SecretString

setattr(Credentials, '__module__', 'dex_connectors.west.client')
