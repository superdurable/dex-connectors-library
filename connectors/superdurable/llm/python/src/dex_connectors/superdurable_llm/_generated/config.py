# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    provider: str
    base_url: str
    model: str
    anthropic_workspace_id: str
    max_response_bytes: int
    region: str = "global"
    openai_compatible_api: str = "chat-completions"
    models: tuple[str, ...] = ()
    model_context_window_tokens: tuple[str, ...] = ()

setattr(Config, '__module__', 'dex_connectors.superdurable_llm.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    api_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.superdurable_llm.client')
