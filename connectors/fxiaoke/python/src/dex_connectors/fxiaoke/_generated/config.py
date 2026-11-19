# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    employee_id: str
    cloud_host: str = "open.fxiaoke.com"
    timeout_seconds: float = 10
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.fxiaoke.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    app_id: str
    app_secret: SecretString
    permanent_code: SecretString

setattr(Credentials, '__module__', 'dex_connectors.fxiaoke.client')
