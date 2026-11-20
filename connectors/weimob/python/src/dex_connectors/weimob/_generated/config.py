# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://dopen.weimob.com"
    timeout_seconds: float = 10
    max_response_bytes: int = 65536

setattr(Config, '__module__', 'dex_connectors.weimob.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    client_id: str
    client_secret: SecretString
    shop_id: str

setattr(Credentials, '__module__', 'dex_connectors.weimob.client')
