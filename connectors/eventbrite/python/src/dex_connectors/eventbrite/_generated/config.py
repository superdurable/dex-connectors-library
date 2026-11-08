# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://www.eventbriteapi.com/v3"
    timeout_seconds: float = 15
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.eventbrite.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    private_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.eventbrite.client')
