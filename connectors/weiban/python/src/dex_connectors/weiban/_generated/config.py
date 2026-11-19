# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://open.weibanzhushou.com"
    timeout_seconds: float = 15
    max_response_bytes: int = 262144

setattr(Config, '__module__', 'dex_connectors.weiban.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    corp_id: str
    secret: SecretString

setattr(Credentials, '__module__', 'dex_connectors.weiban.client')
