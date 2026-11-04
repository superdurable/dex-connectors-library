# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    user_info_url: str = "https://api.linkedin.com/v2/userinfo"
    timeout_seconds: float = 15
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.linkedin.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    oauth_client_id: str
    oauth_client_secret: SecretString
    access_token: SecretString
    refresh_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.linkedin.client')
