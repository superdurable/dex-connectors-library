# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    authorization_endpoint: str
    token_endpoint: str
    scopes: tuple[str, ...]
    api_base_url: str
    token_endpoint_auth_method: str = "client_secret_post"
    max_body_bytes: int = 1048576
    request_timeout_seconds: float = 12

setattr(Config, '__module__', 'dex_connectors.superdurable_oauth2_http.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    oauth_client_id: str
    oauth_client_secret: SecretString
    access_token: SecretString
    refresh_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.superdurable_oauth2_http.client')
