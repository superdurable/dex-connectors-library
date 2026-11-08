# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://api.dropboxapi.com"
    content_base_url: str = "https://content.dropboxapi.com"
    timeout_seconds: float = 15
    max_response_bytes: int = 2097152
    max_download_bytes: int = 1048576
    max_upload_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.dropbox.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    oauth_client_id: str
    oauth_client_secret: SecretString
    access_token: SecretString
    refresh_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.dropbox.client')
