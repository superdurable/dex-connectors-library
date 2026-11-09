# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    customer_id: str
    base_url: str = "https://campaign.api.bingads.microsoft.com"
    timeout_seconds: float = 15
    max_response_bytes: int = 4194304

setattr(Config, '__module__', 'dex_connectors.microsoft_advertising.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    oauth_client_id: str
    oauth_client_secret: SecretString
    developer_token: SecretString
    access_token: SecretString
    refresh_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.microsoft_advertising.client')
