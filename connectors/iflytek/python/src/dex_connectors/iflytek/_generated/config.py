# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://spark-api-open.xf-yun.com"
    timeout_seconds: float = 45
    max_response_bytes: int = 131072
    model: str = "lite"
    max_output_tokens: int = 512

setattr(Config, '__module__', 'dex_connectors.iflytek.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    api_password: SecretString

setattr(Credentials, '__module__', 'dex_connectors.iflytek.client')
