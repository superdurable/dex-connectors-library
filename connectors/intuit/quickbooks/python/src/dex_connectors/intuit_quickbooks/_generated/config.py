# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    realm_id: str
    environment: str = "production"
    max_response_bytes: int = 4194304

setattr(Config, '__module__', 'dex_connectors.intuit_quickbooks.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    client_id: str
    client_secret: SecretString
    access_token: SecretString
    refresh_token: SecretString
    id_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.intuit_quickbooks.client')
