# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    endpoint: str
    default_queue_url: str
    region: str = "us-east-1"
    max_messages_per_receive: int = 10
    receive_wait_seconds: int = 20
    max_response_bytes: int = 4194304

setattr(Config, '__module__', 'dex_connectors.amazon_sqs.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    access_key_id: SecretString
    secret_access_key: SecretString
    session_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.amazon_sqs.client')
