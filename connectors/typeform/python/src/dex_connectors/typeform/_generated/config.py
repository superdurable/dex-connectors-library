# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    data_center: str = "us"
    max_response_bytes: int = 1048576
    webhook_max_body_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.typeform.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    auth_method_id: str = "personal-access-token"
    access_token: SecretString | None = None
    webhook_secret: SecretString = SecretString('')

setattr(Credentials, '__module__', 'dex_connectors.typeform.client')
