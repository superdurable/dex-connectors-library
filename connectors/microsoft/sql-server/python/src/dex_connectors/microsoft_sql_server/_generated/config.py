# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    host: str
    database: str
    user: str
    port: int = 1433
    encrypt: str = "mandatory"
    connect_timeout_seconds: float = 10
    statement_timeout_seconds: float = 5
    max_rows: int = 1000
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.microsoft_sql_server.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    password: SecretString

setattr(Credentials, '__module__', 'dex_connectors.microsoft_sql_server.client')
