# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    account_sid: str
    default_sender: str = ''
    endpoint: str = "https://api.twilio.com/2010-04-01"
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.twilio_messaging.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    auth_method_id: str = "auth-token"
    auth_token: SecretString | None = None
    api_key_sid: str | None = None
    api_key_secret: SecretString | None = None

setattr(Credentials, '__module__', 'dex_connectors.twilio_messaging.client')
