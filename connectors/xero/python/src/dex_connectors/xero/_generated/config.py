# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    max_response_bytes: int = 4194304
    organisation: str = ''

setattr(Config, '__module__', 'dex_connectors.xero.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    auth_method_id: str = "custom-connection"
    client_id: str | None = None
    client_secret: SecretString | None = None
    access_token: SecretString = SecretString('')
    refresh_token: SecretString | None = None

setattr(Credentials, '__module__', 'dex_connectors.xero.client')
