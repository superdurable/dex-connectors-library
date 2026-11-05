# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:
    endpoint: str = "https://api.hubapi.com"
    max_response_bytes: int = 1048576


setattr(Config, "__module__", "dex_connectors.hubspot.client")


@dataclass(frozen=True, slots=True)
class Credentials:
    auth_method_id: str = "private-app-token"
    access_token: SecretString | None = None
    oauth_client_id: str | None = None
    oauth_client_secret: SecretString | None = None
    refresh_token: SecretString | None = None


setattr(Credentials, "__module__", "dex_connectors.hubspot.client")
