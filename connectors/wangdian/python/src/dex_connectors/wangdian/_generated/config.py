# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://openapi.huice.com"
    timeout_seconds: float = 10
    max_response_bytes: int = 2097152

setattr(Config, '__module__', 'dex_connectors.wangdian.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    sid: SecretString
    app_key: SecretString
    app_secret: SecretString

setattr(Credentials, '__module__', 'dex_connectors.wangdian.client')
