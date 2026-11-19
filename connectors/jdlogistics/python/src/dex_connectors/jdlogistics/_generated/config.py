# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://api.jdl.com"
    timeout_seconds: float = 15
    max_response_bytes: int = 65536

setattr(Config, '__module__', 'dex_connectors.jdlogistics.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    app_key: str
    app_secret: SecretString
    access_token: SecretString
    customer_code: str

setattr(Credentials, '__module__', 'dex_connectors.jdlogistics.client')
