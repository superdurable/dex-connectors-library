# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    max_response_bytes: int = 4194304
    webhook_max_body_bytes: int = 4194304

setattr(Config, '__module__', 'dex_connectors.helpscout.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    app_id: str
    app_secret: SecretString
    access_token: SecretString
    webhook_secret: SecretString

setattr(Credentials, '__module__', 'dex_connectors.helpscout.client')
