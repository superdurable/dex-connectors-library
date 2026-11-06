# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    docs_endpoint: str = "https://docs.googleapis.com"
    drive_endpoint: str = "https://www.googleapis.com"
    max_response_bytes: int = 8388608
    max_text_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.google_docs.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    auth_method_id: str = "google-oauth"
    oauth_client_id: str | None = None
    oauth_client_secret: SecretString | None = None
    access_token: SecretString | None = None
    refresh_token: SecretString | None = None
    service_account_key: SecretString | None = None
    delegated_user: str | None = None

setattr(Credentials, '__module__', 'dex_connectors.google_docs.client')
