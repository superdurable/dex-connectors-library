# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://api.caiyunapp.com"
    timeout_seconds: float = 10
    max_response_bytes: int = 262144

setattr(Config, '__module__', 'dex_connectors.caiyun.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    app_key: SecretString
    app_secret: SecretString

setattr(Credentials, '__module__', 'dex_connectors.caiyun.client')
