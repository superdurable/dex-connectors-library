# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    base_url: str = "https://qyapi.weixin.qq.com"
    timeout_seconds: float = 10
    max_response_bytes: int = 262144

setattr(Config, '__module__', 'dex_connectors.tencent_wecom.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    auth_method_id: str = "group-robot"
    webhook_key: SecretString | None = None
    corp_id: str | None = None
    agent_id: str | None = None
    corp_secret: SecretString | None = None

setattr(Credentials, '__module__', 'dex_connectors.tencent_wecom.client')
