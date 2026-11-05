# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    account_identifier: str
    warehouse: str = ''
    role: str = ''
    database: str = ''
    schema: str = ''
    statement_timeout_seconds: int = 3600
    max_rows: int = 1000
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.snowflake.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    auth_method_id: str = "key-pair"
    user: str | None = None
    private_key: SecretString | None = None
    programmatic_access_token: SecretString | None = None

setattr(Credentials, '__module__', 'dex_connectors.snowflake.client')
