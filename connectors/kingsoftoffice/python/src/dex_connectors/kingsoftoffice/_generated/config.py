# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://openapi.wps.cn"
    timeout_seconds: float = 10
    max_response_bytes: int = 1048576
    page_size: int = 100
    request_signing_enabled: bool = False

setattr(Config, '__module__', 'dex_connectors.kingsoftoffice.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    app_id: str
    app_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.kingsoftoffice.client')
