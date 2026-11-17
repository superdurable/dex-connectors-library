# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://app.cloopen.com:8883"
    timeout_seconds: float = 10
    max_response_bytes: int = 65536

setattr(Config, '__module__', 'dex_connectors.cloopen.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    account_sid: SecretString
    auth_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.cloopen.client')
