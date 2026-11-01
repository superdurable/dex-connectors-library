# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated connection settings and credential values."""

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:
    """OpenAI connection settings from the manifest."""

    model: str = "gpt-6-sol"
    endpoint: str = "https://api.openai.com/v1"
    max_response_bytes: int = 8388608
    max_sse_event_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.openai.client')


@dataclass(frozen=True, slots=True)
class Credentials:
    """OpenAI provider credentials resolved per call."""

    api_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.openai.client')
