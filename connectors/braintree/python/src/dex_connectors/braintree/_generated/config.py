# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://payments.sandbox.braintree-api.com"
    api_version: str = "2019-01-01"
    timeout_seconds: float = 15
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.braintree.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    public_key: str
    private_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.braintree.client')
