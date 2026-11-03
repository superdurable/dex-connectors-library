# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    endpoint: str = "https://gmail.googleapis.com/gmail/v1"
    max_response_bytes: int = 1048576
    max_message_bytes: int = 10485760
    poll_interval_seconds: float = 10
    poll_page_size: int = 25

setattr(Config, '__module__', 'dex_connectors.google_gmail.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    auth_method_id: str = "google-oauth"
    oauth_client_id: str | None = None
    oauth_client_secret: SecretString | None = None
    access_token: SecretString | None = None
    refresh_token: SecretString | None = None
    primary_email: str | None = None
    service_account_key: SecretString | None = None
    delegated_user: str | None = None

setattr(Credentials, '__module__', 'dex_connectors.google_gmail.client')
