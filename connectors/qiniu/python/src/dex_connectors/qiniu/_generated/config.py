# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://uc.qiniuapi.com"
    timeout_seconds: float = 10
    max_response_bytes: int = 262144

setattr(Config, '__module__', 'dex_connectors.qiniu.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    access_key: SecretString
    secret_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.qiniu.client')
