# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated connection settings and credential values."""

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:
    """Gmail connection settings from the manifest."""

    endpoint: str = "https://gmail.googleapis.com/gmail/v1"
    max_response_bytes: int = 1048576
    max_message_bytes: int = 10485760
    poll_interval_seconds: float = 10
    poll_page_size: int = 25

setattr(Config, '__module__', 'dex_connectors.google_gmail.client')


@dataclass(frozen=True, slots=True)
class Credentials:
    """Gmail provider credentials resolved per call."""

    oauth_client_id: str
    oauth_client_secret: SecretString
    access_token: SecretString
    refresh_token: SecretString
    primary_email: str
    service_account_key: SecretString
    delegated_user: str

setattr(Credentials, '__module__', 'dex_connectors.google_gmail.client')
