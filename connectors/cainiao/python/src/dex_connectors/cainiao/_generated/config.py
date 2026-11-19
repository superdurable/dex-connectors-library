# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://eco.taobao.com/router/rest"
    timeout_seconds: float = 15
    max_response_bytes: int = 65536

setattr(Config, '__module__', 'dex_connectors.cainiao.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    app_key: str
    app_secret: SecretString
    session_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.cainiao.client')
