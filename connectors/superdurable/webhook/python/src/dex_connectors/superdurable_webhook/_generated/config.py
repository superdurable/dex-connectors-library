# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    signature_prefix: str
    event_id_header: str
    event_id_pointer: str
    forwarded_headers: tuple[str, ...]
    delivery_url: str
    verification: str = "hmacSha256"
    signature_header: str = "X-Signature-256"
    signature_encoding: str = "hex"
    timestamp_tolerance_seconds: int = 300
    token_header: str = "X-Webhook-Token"
    max_body_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.superdurable_webhook.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    signing_secret: SecretString

setattr(Credentials, '__module__', 'dex_connectors.superdurable_webhook.client')
