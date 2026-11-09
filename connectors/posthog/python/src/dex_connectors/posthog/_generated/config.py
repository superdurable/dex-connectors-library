# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    capture_url: str = "https://us.i.posthog.com"
    app_url: str = "https://us.posthog.com"
    timeout_seconds: float = 15
    max_response_bytes: int = 2097152

setattr(Config, '__module__', 'dex_connectors.posthog.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    project_token: SecretString
    personal_api_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.posthog.client')
