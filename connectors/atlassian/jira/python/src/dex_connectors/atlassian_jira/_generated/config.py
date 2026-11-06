# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    cloud_id: str
    endpoint: str = "https://api.atlassian.com"
    max_response_bytes: int = 4194304

setattr(Config, '__module__', 'dex_connectors.atlassian_jira.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    oauth_client_id: str
    oauth_client_secret: SecretString
    access_token: SecretString
    refresh_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.atlassian_jira.client')
