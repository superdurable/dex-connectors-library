# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://ecs.cn-beijing.volcengineapi.com"
    timeout_seconds: float = 10
    max_response_bytes: int = 262144
    region: str = "cn-beijing"
    service: str = "ecs"

setattr(Config, '__module__', 'dex_connectors.volcengine.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    access_key_id: SecretString
    secret_access_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.volcengine.client')
