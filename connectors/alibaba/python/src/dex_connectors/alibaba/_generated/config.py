# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://ecs.cn-hangzhou.aliyuncs.com"
    timeout_seconds: float = 10
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.alibaba.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    access_key_id: SecretString
    access_key_secret: SecretString

setattr(Credentials, '__module__', 'dex_connectors.alibaba.client')
