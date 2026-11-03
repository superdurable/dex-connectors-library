# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go-derived provider selection and credentialed address policy cases."""

import socket

import pytest
from dex_connectors.sdk import SecretString
from dex_connectors.sdk.providerhttp import AddressRefusal

from dex_connectors.superdurable_llm import Client, Config, Credentials
from dex_connectors.superdurable_llm.client import _check_loopback


class InertCredentials:
    async def resolve(self, call: object) -> Credentials:
        return Credentials(SecretString("sk-inert-unit-test"))


@pytest.mark.parametrize(
    "provider",
    ("openai", "anthropic", "gemini", "qwen", "deepseek", "meta", "mistral", "kimi", "xai"),
)
def test_each_fixed_provider_builds_its_own_wire_format(provider: str) -> None:
    """All providers declared by Go can bind the shared text generation Query."""
    client = Client(Config(provider, "", "mock-model", "", 0), InertCredentials())
    assert client.generate_text().definition().operation.operation_id == "generateText"
    assert client.generate_text().request_features().supports_structured_output


def test_custom_public_and_loopback_hosts_select_their_address_policies() -> None:
    """Public HTTPS stays restricted; a local gateway uses loopback transport checks."""
    credentials = InertCredentials()
    public = Client(Config("openai-compatible", "https://example.com/v1", "mock-model", "", 0), credentials)
    loopback = Client(Config("openai-compatible", "http://127.0.0.1/v1", "mock-model", "", 0), credentials)
    assert public.generate_text()._config.address_policy == "publicHTTPS"
    assert loopback.generate_text()._config.address_policy == "unrestricted"


@pytest.mark.parametrize(
    "address",
    ("8.8.8.8", "10.0.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "fd00:ec2::254", "fe80::1"),
)
def test_nonloopback_address_cannot_receive_local_gateway_key(address: str) -> None:
    """Mirror Go's local gateway address refusal before sending credentials."""
    with pytest.raises(AddressRefusal):
        _check_loopback(address)


def test_localhost_must_resolve_only_to_loopback(monkeypatch: pytest.MonkeyPatch) -> None:
    """Reject a rebinding answer before a credentialed local gateway exchange."""

    def mixed_addresses(*args: object, **kwargs: object) -> list[tuple[object, ...]]:
        return [
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("127.0.0.1", 0)),
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("10.0.0.1", 0)),
        ]

    monkeypatch.setattr(socket, "getaddrinfo", mixed_addresses)
    with pytest.raises(AddressRefusal):
        _check_loopback("localhost")


def test_unrelated_workspace_and_base_url_are_rejected() -> None:
    """Prevent cross-provider settings from silently changing destinations."""
    with pytest.raises(ValueError, match="baseUrl applies only"):
        Client(Config("openai", "https://example.com/v1", "mock-model", "", 0), InertCredentials())
    with pytest.raises(ValueError, match="anthropicWorkspaceId applies only"):
        Client(Config("gemini", "", "mock-model", "wrkspc_123", 0), InertCredentials())
