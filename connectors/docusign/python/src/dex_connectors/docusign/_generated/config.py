# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    account_id: str = ''
    max_response_bytes: int = 1048576
    max_document_bytes: int = 67108864
    connect_max_body_bytes: int = 2097152

setattr(Config, '__module__', 'dex_connectors.docusign.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    auth_method_id: str = "docusign-oauth"
    oauth_client_id: str | None = None
    oauth_client_secret: SecretString | None = None
    access_token: SecretString | None = None
    refresh_token: SecretString | None = None
    connect_hmac_key: SecretString = SecretString('')

setattr(Credentials, '__module__', 'dex_connectors.docusign.client')
