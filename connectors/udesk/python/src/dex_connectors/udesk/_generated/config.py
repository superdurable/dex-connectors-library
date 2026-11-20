# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str
    timeout_seconds: float = 10
    max_response_bytes: int = 262144

setattr(Config, '__module__', 'dex_connectors.udesk.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    admin_email: SecretString
    api_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.udesk.client')
