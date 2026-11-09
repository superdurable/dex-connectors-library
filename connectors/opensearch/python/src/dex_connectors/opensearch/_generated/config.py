# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    cluster_url: str
    timeout_seconds: float = 15
    max_response_bytes: int = 4194304
    max_document_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.opensearch.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    username: str
    password: SecretString

setattr(Credentials, '__module__', 'dex_connectors.opensearch.client')
