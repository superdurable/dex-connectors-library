# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    endpoint: str = "https://discord.com/api/v10"
    max_response_bytes: int = 4194304

setattr(Config, '__module__', 'dex_connectors.discord.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    bot_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.discord.client')
