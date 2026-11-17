# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://api.github.com"
    api_version: str = "2026-03-10"
    timeout_seconds: float = 15
    max_response_bytes: int = 2097152
    default_repository_limit: int = 100
    max_repositories: int = 500
    max_pull_request_body_characters: int = 4000
    max_patch_characters: int = 4000
    max_commit_message_characters: int = 4000

setattr(Config, '__module__', 'dex_connectors.github.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    auth_method_id: str = "github-oauth"
    oauth_client_id: str | None = None
    oauth_client_secret: SecretString | None = None
    access_token: SecretString | None = None
    refresh_token: SecretString = SecretString('')

setattr(Credentials, '__module__', 'dex_connectors.github.client')
