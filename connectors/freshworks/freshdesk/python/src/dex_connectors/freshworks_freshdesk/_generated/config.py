# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    domain: str
    max_response_bytes: int = 4194304

setattr(Config, '__module__', 'dex_connectors.freshworks_freshdesk.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    api_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.freshworks_freshdesk.client')
