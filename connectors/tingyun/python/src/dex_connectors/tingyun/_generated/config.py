# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    api_host: str
    timeout_seconds: float = 10
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.tingyun.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    access_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.tingyun.client')
