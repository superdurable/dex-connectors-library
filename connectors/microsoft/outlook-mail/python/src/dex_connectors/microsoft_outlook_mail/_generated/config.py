# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    max_response_bytes: int = 4194304
    mailbox: str = ''

setattr(Config, '__module__', 'dex_connectors.microsoft_outlook_mail.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    auth_method_id: str = "microsoft-oauth"
    client_id: str | None = None
    client_secret: SecretString | None = None
    access_token: SecretString | None = None
    refresh_token: SecretString | None = None
    tenant_id: str | None = None

setattr(Credentials, '__module__', 'dex_connectors.microsoft_outlook_mail.client')
