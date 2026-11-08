# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://storage.googleapis.com"
    timeout_seconds: float = 30
    max_response_bytes: int = 2097152
    max_upload_bytes: int = 2097152

setattr(Config, '__module__', 'dex_connectors.google_cloud_storage.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    oauth_client_id: str
    oauth_client_secret: SecretString
    access_token: SecretString
    refresh_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.google_cloud_storage.client')
