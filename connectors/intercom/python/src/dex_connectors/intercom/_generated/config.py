# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    region: str = "us"
    max_response_bytes: int = 4194304
    webhook_max_body_bytes: int = 2097152

setattr(Config, '__module__', 'dex_connectors.intercom.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    access_token: SecretString
    client_secret: SecretString

setattr(Credentials, '__module__', 'dex_connectors.intercom.client')
