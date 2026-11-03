# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    account_id: str
    region: str = ''
    workspace_id: str = ''

setattr(Config, '__module__', 'dex_connectors.multi_auth_fixture.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    auth_method_id: str = "google-oauth"
    oauth_client_id: str | None = None
    oauth_client_secret: SecretString | None = None
    access_token: SecretString | None = None
    refresh_token: SecretString | None = None
    service_account_key: SecretString | None = None
    delegated_user: str | None = None
    session_note: str | None = None

setattr(Credentials, '__module__', 'dex_connectors.multi_auth_fixture.client')
