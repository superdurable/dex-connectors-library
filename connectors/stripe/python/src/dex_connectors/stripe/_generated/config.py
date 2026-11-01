# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated connection settings and credential values."""

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:
    """Stripe connection settings from the manifest."""

    endpoint: str = "https://api.stripe.com/v1"
    max_response_bytes: int = 1048576
    webhook_max_body_bytes: int = 1048576
    webhook_signature_tolerance_seconds: float = 300

setattr(Config, '__module__', 'dex_connectors.stripe.client')


@dataclass(frozen=True, slots=True)
class Credentials:
    """Stripe provider credentials resolved per call."""

    secret_key: SecretString
    webhook_secret: SecretString

setattr(Credentials, '__module__', 'dex_connectors.stripe.client')
