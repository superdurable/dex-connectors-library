# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://member.bilibili.com"
    timeout_seconds: float = 15
    max_response_bytes: int = 32768

setattr(Config, '__module__', 'dex_connectors.bilibili.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    client_id: str
    app_secret: SecretString
    access_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.bilibili.client')
