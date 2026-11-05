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
    webhook_signing_key: SecretString = SecretString('')
    service_account_key: SecretString | None = None
    delegated_user: str | None = None
    session_note: str = ''
    environment: str = ''
    login_url: str = ''
    request_count: int | None = None
    wait_duration: int | None = None
    labels: list[str] | None = None
    headers: dict[str, str] | None = None
    optional_count: int | None = None
    optional_note: str = ''
    tenant_id: str | None = None
    client_id: str | None = None
    client_secret: SecretString | None = None

setattr(Credentials, '__module__', 'dex_connectors.multi_auth_fixture.client')
