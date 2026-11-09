# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    environment_url: str
    tenant_id: str
    client_id: str
    timeout_seconds: float = 15
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.microsoft_dynamics_365_sales.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    client_secret: SecretString

setattr(Credentials, '__module__', 'dex_connectors.microsoft_dynamics_365_sales.client')
