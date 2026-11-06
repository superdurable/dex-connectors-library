# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    endpoint: str = "https://slack.com/api"
    max_response_bytes: int = 4194304
    max_message_characters: int = 40000

setattr(Config, '__module__', 'dex_connectors.slack.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    oauth_client_id: str
    oauth_client_secret: SecretString
    bot_token: SecretString
    bot_refresh_token: SecretString
    user_token: SecretString
    user_refresh_token: SecretString
    app_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.slack.client')
