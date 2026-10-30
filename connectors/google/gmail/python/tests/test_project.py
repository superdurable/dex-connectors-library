# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gmail Dex Web connection loading and local OAuth writeback."""

import json
from datetime import UTC, datetime, timedelta
from pathlib import Path
from uuid import uuid4

import httpx
import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, OperationRef
from dex_connectors.sdk.projectconfig import (
    Configuration,
    ConfigurationStore,
    ConnectionConfiguration,
    InMemoryObjectStore,
    Scope,
    load_dex_web_project,
    load_project_from_object_store,
)
from dex_connectors.sdk.projectconfig.models import CredentialBrokerConfiguration

from dex_connectors.google_gmail import new_project_connection, new_project_provider


@pytest.mark.asyncio
async def test_project_provider_refreshes_and_persists_local_oauth_credentials(tmp_path: Path) -> None:
    """A rotated Google grant is written back through the SDK's Dex Web store."""
    path = tmp_path / "connections.json"
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "gmail",
                        "connectionName": "support",
                        "modulePath": (
                            "github.com/superdurable/dex-connectors-library/"
                            "connectors/google/gmail"
                        ),
                        "provider": "google",
                        "configuration": {"maxResponseBytes": 2048},
                        "authMethodId": "google-oauth",
                        "credentials": {
                            "auth_method": "google-oauth",
                            "oauth_client_id": "client-id",
                            "oauth_client_secret": "inert-client-secret",
                            "access_token": "old-access",
                            "refresh_token": "old-refresh",
                            "primary_email": "owner@example.org",
                        },
                        "credentialExpiresAt": (datetime.now(UTC) - timedelta(minutes=10)).isoformat(),
                    }
                ],
                "triggerBindings": [],
            }
        )
    )
    path.chmod(0o600)
    project = await load_dex_web_project(path)

    def answer(request: httpx.Request) -> httpx.Response:
        assert str(request.url) == "https://oauth2.googleapis.com/token"
        return httpx.Response(
            200,
            json={
                "access_token": "new-access",
                "refresh_token": "new-refresh",
                "expires_in": 3600,
                "token_type": "Bearer",
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        connection = new_project_connection(project, "support", http_client=http_client)
        assert connection.reference == ConnectionRef("google", "support")
        provider = connection.client
        assert provider is not None
        call = Call(
            CallID(str(uuid4())), ConnectionRef("google", "support"), OperationRef("gmail", "getMessage")
        )
        credential = await provider.resolve_credentials(call)
        await connection.close()
    assert provider.max_response_bytes == 2048
    assert credential.access_token.reveal() == "new-access"
    assert credential.refresh_token.reveal() == "new-refresh"
    stored = json.loads(path.read_text())["connections"][0]["credentials"]
    assert stored["access_token"] == "new-access"
    assert stored["refresh_token"] == "new-refresh"
    assert path.stat().st_mode & 0o777 == 0o600
    await project.close()


@pytest.mark.asyncio
async def test_project_provider_rejects_missing_connection(tmp_path: Path) -> None:
    """An undeclared static connection name fails before any credential lookup."""
    path = tmp_path / "connections.json"
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [],
                "triggerBindings": [],
            }
        )
    )
    path.chmod(0o600)
    project = await load_dex_web_project(path)
    with pytest.raises(ValueError):
        new_project_provider(project, "support")
    await project.close()


@pytest.mark.asyncio
async def test_shared_connection_uses_broker_and_rotates_after_rejection() -> None:
    """Broker material without host-kept OAuth secrets authenticates and refreshes after 401."""
    scope = Scope("gmail-test", "preview")
    objects = InMemoryObjectStore()
    configurations = ConfigurationStore(objects, scope)
    token_reference = await configurations.create_credential_broker_token("inert-broker-token")
    configuration = Configuration(
        scope,
        1,
        (
            ConnectionConfiguration(
                "gmail",
                "support",
                "github.com/superdurable/dex-connectors-library/connectors/google/gmail",
                "google",
                {},
                shared_connection_id="shared-gmail-7",
            ),
        ),
        credential_broker=CredentialBrokerConfiguration("https://broker.example/read", token_reference),
    )
    written, _ = await configurations.write_configuration(0, configuration)
    snapshot = await configurations.freeze_configuration(written.revision)
    broker_requests: list[dict[str, object]] = []
    gmail_tokens: list[str] = []

    def broker_answer(request: httpx.Request) -> httpx.Response:
        broker_requests.append(json.loads(request.content))
        assert request.headers["Authorization"] == "Bearer inert-broker-token"
        return httpx.Response(
            200,
            json={
                "status": "READY",
                "credential": {
                    "auth_method": "google-oauth",
                    "oauth_client_id": "client-id",
                    "access_token": f"shared-access-{len(broker_requests)}",
                    "primary_email": "owner@example.org",
                },
                "revision": len(broker_requests),
                "expiresAt": (datetime.now(UTC) + timedelta(hours=1)).isoformat(),
            },
        )

    def gmail_answer(request: httpx.Request) -> httpx.Response:
        gmail_tokens.append(request.headers["Authorization"])
        return httpx.Response(401 if len(gmail_tokens) == 1 else 200, json={"id": "message-1"})

    async with (
        httpx.AsyncClient(transport=httpx.MockTransport(broker_answer)) as broker_client,
        httpx.AsyncClient(transport=httpx.MockTransport(gmail_answer)) as gmail_client,
    ):
        project = await load_project_from_object_store(objects, scope, snapshot, broker_client)
        provider = new_project_provider(project, "support", http_client=gmail_client)
        call = Call(
            CallID(str(uuid4())), ConnectionRef("google", "support"), OperationRef("gmail", "getMessage")
        )
        credential = await provider.resolve_credentials(call)
        assert credential.access_token.reveal() == "shared-access-1"
        assert credential.oauth_client_secret.reveal() == ""
        assert credential.refresh_token.reveal() == ""
        response, replacement = await provider.request(
            call, credential, "GET", "/users/me/messages/message-1"
        )
        assert response.status_code == 200
        assert replacement.access_token.reveal() == "shared-access-2"
        assert gmail_tokens == ["Bearer shared-access-1", "Bearer shared-access-2"]
        assert broker_requests == [
            {"sharedConnectionId": "shared-gmail-7"},
            {"sharedConnectionId": "shared-gmail-7", "rejectedRevision": 1},
        ]
        await project.close()
