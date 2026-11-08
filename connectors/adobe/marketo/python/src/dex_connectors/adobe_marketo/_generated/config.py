# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    rest_endpoint: str
    token_endpoint: str
    timeout_seconds: float = 15
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.adobe_marketo.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    client_id: str
    client_secret: SecretString
    access_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.adobe_marketo.client')
