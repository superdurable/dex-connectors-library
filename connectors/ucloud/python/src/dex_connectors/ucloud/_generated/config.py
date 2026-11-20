# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://api.ucloud.cn"
    timeout_seconds: float = 10
    max_response_bytes: int = 262144

setattr(Config, '__module__', 'dex_connectors.ucloud.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    public_key: SecretString
    private_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.ucloud.client')
