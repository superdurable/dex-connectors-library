# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    database: str
    timeout_seconds: float = 15
    max_documents: int = 100
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.mongodb.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    connection_uri: SecretString

setattr(Credentials, '__module__', 'dex_connectors.mongodb.client')
