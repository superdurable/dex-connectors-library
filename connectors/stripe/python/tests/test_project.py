# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Project connection settings and call-time secret resolution."""

from uuid import UUID

import httpx
import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, OperationRef
from dex_connectors.sdk.projectconfig import (
    Configuration,
    ConfigurationStore,
    ConnectionConfiguration,
    ConnectionKey,
    ConnectionStore,
    CredentialMaterial,
    InMemoryObjectStore,
    LoadedProject,
    Scope,
    SnapshotRef,
)

from dex_connectors.stripe import GetCheckoutSessionInput, decode_credentials, new_project_client


def project_with_local_key(material: dict[str, str]) -> LoadedProject:
    """Construct a loaded local project with frozen settings and current secret material."""
    scope = Scope("local", "preview")
    store = InMemoryObjectStore()
    entry = ConnectionConfiguration(
        "stripe",
        "stripe-payments",
        "github.com/superdurable/dex-connectors-library/connectors/stripe",
        "stripe",
        {"endpoint": "http://127.0.0.1:12345/v1", "maxResponseBytes": 256},
        "api-key-only",
    )
    project = LoadedProject(
        Configuration(scope, 1, (entry,)),
        SnapshotRef(f"{scope.prefix}/configuration/head", "version-1", "sha256:" + "0" * 64),
        ConnectionStore(store, scope),
        ConfigurationStore(store, scope),
    )
    project.local_credentials = {
        ConnectionKey("stripe", "stripe-payments"): CredentialMaterial(material, None, "api-key-only")
    }
    return project


def test_credential_decoder_preserves_legacy_webhook_method() -> None:
    """Pre-auth-method credentials still require their webhook signing secret."""
    credential = decode_credentials({"secret_key": "sk_test_example", "webhook_secret": "whsec_example"})
    assert credential.auth_method_id == "api-key-with-webhook"
    assert "sk_test_example" not in repr(credential)
    with pytest.raises(ValueError):
        decode_credentials({"secret_key": "sk_test_example"})
    assert (
        decode_credentials({"auth_method": "api-key-only", "secret_key": "sk_test_example"}).webhook_secret
        is None
    )


async def test_project_client_uses_current_local_credential_at_call_time() -> None:
    """Local Dex Web secret rotation is observed without rebuilding the client."""
    project = project_with_local_key({"secret_key": "sk_test_first"})
    keys: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        keys.append(request.headers["Authorization"])
        return httpx.Response(200, json={"id": "cs_test_123", "object": "checkout.session"})

    client = new_project_client(
        project, "stripe-payments", httpx.AsyncClient(transport=httpx.MockTransport(answer))
    )
    call = Call(
        CallID(str(UUID("ffdd13f6-fd46-4d55-ad11-7afce245c321"))),
        ConnectionRef("stripe", "stripe-payments"),
        OperationRef("stripe", "getCheckoutSession"),
    )
    first = await client.get_checkout_session().invoke(call, GetCheckoutSessionInput("cs_test_123"))
    project.local_credentials[ConnectionKey("stripe", "stripe-payments")] = CredentialMaterial(
        {"secret_key": "sk_test_second"}, None, "api-key-only"
    )
    second = await client.get_checkout_session().invoke(call, GetCheckoutSessionInput("cs_test_123"))
    assert first.branch == second.branch == "found"
    assert keys[0] != keys[1]
    assert "sk_test_first" not in repr(client) and "sk_test_second" not in repr(client)


def test_project_client_rejects_unknown_settings() -> None:
    """Unknown connection configuration cannot silently change the provider boundary."""
    project = project_with_local_key({"secret_key": "sk_test_example"})
    entry = project.configuration.connections[0]
    entry.configuration["unexpected"] = "value"
    with pytest.raises(ValueError, match="unknown fields"):
        new_project_client(project, "stripe-payments")
