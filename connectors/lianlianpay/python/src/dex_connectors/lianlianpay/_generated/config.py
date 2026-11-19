# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://openapi.lianlianpay.com"
    timeout_seconds: float = 15
    max_response_bytes: int = 262144

setattr(Config, '__module__', 'dex_connectors.lianlianpay.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    merchant_id: str
    private_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.lianlianpay.client')
