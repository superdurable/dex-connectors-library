# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    model: str = "gemini-3.5-flash-lite"
    endpoint: str = "https://generativelanguage.googleapis.com/v1beta"
    max_response_bytes: int = 8388608

setattr(Config, '__module__', 'dex_connectors.google_gemini.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    api_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.google_gemini.client')
