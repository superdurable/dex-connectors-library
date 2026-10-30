# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gmail project credential wire shapes and secret-safe validation."""

import pytest

from dex_connectors.google_gmail import decode_credentials, encode_credentials


def test_oauth_credentials_round_trip_with_go_field_names() -> None:
    """A rotated OAuth token retains all required project credential fields."""
    source = {
        "auth_method": "google-oauth",
        "oauth_client_id": "client-id",
        "oauth_client_secret": "inert-client-secret",
        "access_token": "inert-access-token",
        "refresh_token": "inert-refresh-token",
        "primary_email": "owner@example.org",
    }
    credential = decode_credentials(source)
    assert encode_credentials(credential) == source
    assert "inert-client-secret" not in repr(credential)
    assert "inert-refresh-token" not in repr(credential)


def test_legacy_oauth_material_uses_default_auth_method() -> None:
    """Stored credentials predating auth methods decode as Google OAuth."""
    credential = decode_credentials(
        {
            "oauth_client_id": "client-id",
            "oauth_client_secret": "inert-client-secret",
            "access_token": "inert-access-token",
            "refresh_token": "inert-refresh-token",
            "primary_email": "owner@example.org",
        }
    )
    assert credential.auth_method_id == "google-oauth"


def test_workspace_credentials_round_trip() -> None:
    """A delegated account uses the Go service-account field names."""
    source = {
        "auth_method": "workspace-domain-delegation",
        "service_account_key": "inert-json-key",
        "delegated_user": "owner@example.org",
    }
    assert encode_credentials(decode_credentials(source)) == source


def test_shared_credentials_need_only_host_revealed_fields() -> None:
    """A broker-backed OAuth grant can omit host-kept refresh material."""
    credential = decode_credentials(
        {
            "auth_method": "google-oauth",
            "oauth_client_id": "client-id",
            "access_token": "inert-access-token",
            "primary_email": "owner@example.org",
        },
        shared=True,
    )
    assert credential.access_token.reveal() == "inert-access-token"


@pytest.mark.parametrize(
    "source",
    [
        {"auth_method": "google-oauth", "oauth_client_id": "client-id"},
        {"auth_method": "workspace-domain-delegation", "delegated_user": "owner@example.org"},
        {"auth_method": "unknown", "access_token": "inert"},
        {"auth_method": "google-oauth", "access_token": 123},
        {"auth_method": "google-oauth", "access_token": "inert", "extra": "unsafe"},
    ],
)
def test_invalid_material_fails_before_provider_call(source: dict[str, object]) -> None:
    """Incomplete or unexpected secret fields cannot become live credentials."""
    with pytest.raises(ValueError):
        decode_credentials(source)
