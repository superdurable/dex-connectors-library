# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://open.kuaishou.com"
    timeout_seconds: float = 15
    max_response_bytes: int = 262144
    page_size: int = 20

setattr(Config, '__module__', 'dex_connectors.kuaishou.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    app_id: str
    access_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.kuaishou.client')
