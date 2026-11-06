# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    endpoint: str = "https://graph.microsoft.com"
    max_response_bytes: int = 1048576
    max_text_bytes: int = 1048576
    max_upload_bytes: int = 5242880
    search_page_size: int = 50

setattr(Config, '__module__', 'dex_connectors.microsoft_onedrive.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    auth_method_id: str = "microsoft-oauth"
    oauth_client_id: str | None = None
    oauth_client_secret: SecretString | None = None
    access_token: SecretString | None = None
    refresh_token: SecretString | None = None
    tenant_id: str | None = None
    app_client_id: str | None = None
    app_client_secret: SecretString | None = None

setattr(Credentials, '__module__', 'dex_connectors.microsoft_onedrive.client')
