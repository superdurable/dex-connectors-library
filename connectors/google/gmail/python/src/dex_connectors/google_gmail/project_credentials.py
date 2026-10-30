# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Secret-safe Gmail credential codec for Dex project connection storage."""

from typing import Any

from dex_connectors.sdk import SecretString

from .models import Credentials

_FIELDS = frozenset(
    {
        "auth_method",
        "oauth_client_id",
        "oauth_client_secret",
        "access_token",
        "refresh_token",
        "primary_email",
        "service_account_key",
        "delegated_user",
    }
)


def decode_credentials(value: dict[str, Any], *, shared: bool = False) -> Credentials:
    """Decode Go-compatible project material, validating the selected auth method."""
    if not isinstance(value, dict) or set(value) - _FIELDS:
        raise ValueError("Gmail credential fields are invalid")
    if any(not isinstance(item, str) for item in value.values()):
        raise ValueError("Gmail credential fields are invalid")
    method = value.get("auth_method") or "google-oauth"
    credentials = Credentials(
        access_token=SecretString(value.get("access_token", "")),
        primary_email=value.get("primary_email", ""),
        auth_method_id=method,
        oauth_client_id=value.get("oauth_client_id", ""),
        oauth_client_secret=SecretString(value.get("oauth_client_secret", "")),
        refresh_token=SecretString(value.get("refresh_token", "")),
        service_account_key=SecretString(value.get("service_account_key", "")),
        delegated_user=value.get("delegated_user", ""),
    )
    if method == "google-oauth":
        required: tuple[str, ...] = (
            credentials.oauth_client_id,
            credentials.access_token.reveal(),
            credentials.primary_email,
        )
        if not shared:
            required += (credentials.oauth_client_secret.reveal(), credentials.refresh_token.reveal())
    elif method == "workspace-domain-delegation":
        required = (credentials.delegated_user,)
        if shared:
            required += (credentials.access_token.reveal(),)
        else:
            required += (credentials.service_account_key.reveal(),)
    else:
        raise ValueError("Gmail authorization method is invalid")
    if any(not field for field in required):
        raise ValueError("Gmail credential material is incomplete")
    return credentials


def encode_credentials(credentials: Credentials) -> dict[str, str]:
    """Encode complete replacement material using Go's secret field names."""
    fields = {
        "auth_method": credentials.auth_method_id,
        "oauth_client_id": credentials.oauth_client_id,
        "oauth_client_secret": credentials.oauth_client_secret.reveal(),
        "access_token": credentials.access_token.reveal(),
        "refresh_token": credentials.refresh_token.reveal(),
        "primary_email": credentials.primary_email,
        "service_account_key": credentials.service_account_key.reveal(),
        "delegated_user": credentials.delegated_user,
    }
    return {name: value for name, value in fields.items() if value or name == "auth_method"}


__all__ = ["decode_credentials", "encode_credentials"]
