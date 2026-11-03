# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    endpoint: str = "https://api.airtable.com"
    max_response_bytes: int = 4194304

setattr(Config, '__module__', 'dex_connectors.airtable.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    personal_access_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.airtable.client')
