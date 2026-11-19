# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://api2.ofpay.com"
    timeout_seconds: float = 15
    max_response_bytes: int = 262144

setattr(Config, '__module__', 'dex_connectors.feihan.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    project_id: str
    password: SecretString
    key_str: SecretString

setattr(Credentials, '__module__', 'dex_connectors.feihan.client')
