# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    api_version: str = "v62.0"
    max_response_bytes: int = 1048576
    query_batch_size: int = 200

setattr(Config, '__module__', 'dex_connectors.salesforce.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    auth_method_id: str = "salesforce-oauth"
    oauth_client_id: str | None = None
    oauth_client_secret: SecretString | None = None
    access_token: SecretString | None = None
    refresh_token: SecretString | None = None
    instance_url: str = ''
    jwt_username: str | None = None
    jwt_private_key: SecretString | None = None
    jwt_login_environment: str = ''

setattr(Credentials, '__module__', 'dex_connectors.salesforce.client')
