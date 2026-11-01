# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://api.bitbucket.org/2.0"
    timeout_seconds: float = 15
    max_response_bytes: int = 2097152

setattr(Config, '__module__', 'dex_connectors.atlassian_bitbucket.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    access_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.atlassian_bitbucket.client')
