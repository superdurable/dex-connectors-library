# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    endpoint: str = "https://sheets.googleapis.com/v4"
    max_response_bytes: int = 4194304
    max_rows: int = 10000

setattr(Config, '__module__', 'dex_connectors.google_spreadsheet.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    oauth_client_id: str
    oauth_client_secret: SecretString
    access_token: SecretString
    refresh_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.google_spreadsheet.client')
