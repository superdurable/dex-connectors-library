# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    max_response_bytes: int = 4194304
    webhook_max_body_bytes: int = 1048576
    webhook_signature_tolerance_seconds: float = 60

setattr(Config, '__module__', 'dex_connectors.linear.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    auth_method_id: str = "personal-api-key"
    api_key: SecretString | None = None
    webhook_signing_secret: SecretString = SecretString('')
    oauth_client_id: str | None = None
    oauth_client_secret: SecretString | None = None
    access_token: SecretString | None = None
    refresh_token: SecretString | None = None

setattr(Credentials, '__module__', 'dex_connectors.linear.client')
