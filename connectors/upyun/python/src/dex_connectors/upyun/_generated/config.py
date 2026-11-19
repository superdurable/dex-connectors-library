# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    bucket: str
    base_url: str = "https://v0.api.upyun.com"
    timeout_seconds: float = 15
    max_response_bytes: int = 1024

setattr(Config, '__module__', 'dex_connectors.upyun.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    operator: str
    password: SecretString

setattr(Credentials, '__module__', 'dex_connectors.upyun.client')
