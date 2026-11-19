# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typeform personal access token and webhook secret and their project storage codec."""

from dataclasses import dataclass, field
from typing import Any

from dex_connectors.sdk import SecretString

PERSONAL_ACCESS_TOKEN_AUTH_METHOD_ID = "personal-access-token"
"""The only Typeform authorization method: a personal access token."""

_FIELDS = frozenset({"auth_method", "access_token", "webhook_secret"})


@dataclass(frozen=True, slots=True)
class Credentials:
    """The personal access token and the optional webhook signing secret."""

    access_token: SecretString = field(repr=False)
    webhook_secret: SecretString = field(default_factory=lambda: SecretString(""), repr=False)
    auth_method_id: str = PERSONAL_ACCESS_TOKEN_AUTH_METHOD_ID


def decode_credentials(value: dict[str, Any]) -> Credentials:
    """Decode Go-compatible project material; a blank auth_method uses the token method."""
    if not isinstance(value, dict) or set(value) - _FIELDS:
        raise ValueError("Typeform credential fields are invalid")
    if any(not isinstance(item, str) for item in value.values()):
        raise ValueError("Typeform credential fields are invalid")
    method = value.get("auth_method", "") or PERSONAL_ACCESS_TOKEN_AUTH_METHOD_ID
    if method != PERSONAL_ACCESS_TOKEN_AUTH_METHOD_ID:
        raise ValueError("credential auth_method is invalid")
    if not value.get("access_token"):
        raise ValueError("credential access_token is required")
    return Credentials(
        SecretString(value["access_token"]), SecretString(value.get("webhook_secret", "")), method
    )


__all__ = ["PERSONAL_ACCESS_TOKEN_AUTH_METHOD_ID", "Credentials", "decode_credentials"]
