# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable credential, broker, S3, and Dex Web local-file examples."""

import asyncio
import json
import tempfile
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from pathlib import Path

import httpx

from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    CredentialProvider,
    CredentialRefreshDriver,
    CredentialRefreshResult,
    CredentialRefreshState,
    OperationRef,
    SecretString,
    resolve_credential,
)
from dex_connectors.sdk.projectconfig import (
    Configuration,
    ConfigurationStore,
    ConnectionConfiguration,
    ConnectionKey,
    ConnectionStore,
    CredentialMaterial,
    EnvironmentValue,
    InMemoryObjectStore,
    S3ObjectStore,
    Scope,
    load_dex_web_project,
    load_project_from_object_store,
)
from dex_connectors.sdk.projectconfig.models import CredentialBrokerConfiguration
from dex_connectors.sdk.projectconfig.provider import (
    CredentialProvider as ProjectCredentialProvider,
)
from dex_connectors.sdk.projectconfig.provider import (
    RefreshingCredentialProvider,
)


class ExampleCredentialProvider(CredentialProvider[SecretString]):
    """Resolve one in-memory example secret at call time."""

    async def resolve(self, call: Call) -> SecretString:
        """Check the logical connection without persisting secret material."""
        if not call.id or call.connection.name != "main":
            raise ValueError("unknown example connection")
        return SecretString("REDACTED_EXAMPLE_KEY")


class ExampleRefreshDriver(CredentialRefreshDriver[SecretString]):
    """Show the refresh protocol; this example makes no provider exchange."""

    def refresh_required(self, state: CredentialRefreshState[SecretString]) -> bool:
        """Refresh when the observed expiry is within five minutes."""
        return state.expires_at is not None and state.expires_at <= state.now + timedelta(minutes=5)

    async def refresh(
        self, state: CredentialRefreshState[SecretString]
    ) -> CredentialRefreshResult[SecretString]:
        """Return inert replacement material for the protocol example."""
        return CredentialRefreshResult(SecretString("REDACTED_REPLACEMENT"), state.now + timedelta(hours=1))


class ExampleProjectRefreshDriver(CredentialRefreshDriver[dict[str, str]]):
    """Replace an expired local fixture credential through durable admission."""

    def refresh_required(self, state: CredentialRefreshState[dict[str, str]]) -> bool:
        """Refresh a known expired access token."""
        return state.expires_at is not None and state.expires_at <= state.now

    async def refresh(
        self, state: CredentialRefreshState[dict[str, str]]
    ) -> CredentialRefreshResult[dict[str, str]]:
        """Return complete inert material, retaining the refresh token."""
        return CredentialRefreshResult(
            {"access": "INERT_NEW_ACCESS", "refresh": state.credentials["refresh"]},
            state.now + timedelta(hours=1),
        )


@dataclass
class ExampleS3Client:
    """Small in-process versioned S3 adapter for demonstrating conditional arguments."""

    put_arguments: dict[str, object] | None = None

    def get_bucket_versioning(self, **_: object) -> dict[str, str]:
        """Report versioning enabled, as the constructor requires."""
        return {"Status": "Enabled"}

    def put_object(self, **arguments: object) -> dict[str, str]:
        """Record the precondition and return a stable version identity."""
        self.put_arguments = arguments
        return {"VersionId": "version-1", "ETag": '"etag-1"'}


async def main() -> None:
    """Exercise credential lookup and each project configuration storage path."""
    provider = ExampleCredentialProvider()
    call = Call(
        CallID("00000000-0000-4000-8000-000000000001"),
        ConnectionRef("sample", "main"),
        OperationRef("sample", "getItem"),
    )
    credential = await provider.resolve(call)
    assert credential.reveal() == "REDACTED_EXAMPLE_KEY"
    driver = ExampleRefreshDriver()
    now = datetime.now(UTC)
    state = CredentialRefreshState(credential, now + timedelta(minutes=1), now)
    assert driver.refresh_required(state)
    assert (await driver.refresh(state)).expires_at > now

    scope = Scope("example", "preview")
    objects = InMemoryObjectStore()
    configurations = ConfigurationStore(objects, scope)
    application_secret = await configurations.create_application_secret(
        "WEBHOOK_SECRET", "REDACTED_WEBHOOK_SECRET"
    )
    broker_token = await configurations.create_credential_broker_token("REDACTED_SCOPE_TOKEN")
    entry = ConnectionConfiguration(
        "sample", "main", "example", "sample", {}, shared_connection_id="shared-7"
    )
    document = Configuration(
        scope,
        1,
        (entry,),
        environment={"WEBHOOK_SECRET": EnvironmentValue(secret_ref=application_secret)},
        credential_broker=CredentialBrokerConfiguration("https://broker.example/read", broker_token),
    )
    written, _ = await configurations.write_configuration(0, document)
    snapshot = await configurations.freeze_configuration(written.revision)

    def broker_answer(request: httpx.Request) -> httpx.Response:
        assert request.headers["Authorization"] == "Bearer REDACTED_SCOPE_TOKEN"
        return httpx.Response(
            200, json={"status": "READY", "credential": {"apiKey": "REDACTED_SHARED_KEY"}, "revision": 1}
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(broker_answer)) as http_client:
        project = await load_project_from_object_store(objects, scope, snapshot, http_client)
        assert (await project.resolve_application_environment())[
            "WEBHOOK_SECRET"
        ] == "REDACTED_WEBHOOK_SECRET"
        assert (await project.resolve_credential(ConnectionKey("sample", "main"), "call-1"))[
            "apiKey"
        ] == "REDACTED_SHARED_KEY"
        shared_provider = ProjectCredentialProvider(
            project.connections,
            ConnectionKey("sample", "main"),
            lambda value: {"apiKey": str(value["apiKey"]), "privateKey": str(value["privateKey"])},
            decode_shared=lambda value: {"apiKey": str(value["apiKey"])},
        )
        assert (await shared_provider.resolve(call))["apiKey"] == "REDACTED_SHARED_KEY"
        await project.close()

    with tempfile.TemporaryDirectory() as directory:
        connections_file = Path(directory) / "connections.json"
        connections_file.write_text(
            json.dumps(
                {
                    "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                    "connections": [
                        {
                            "connectorId": "sample",
                            "connectionName": "main",
                            "modulePath": "example",
                            "provider": "sample",
                            "configuration": {},
                            "credentials": {"apiKey": "REDACTED_LOCAL_KEY"},
                        }
                    ],
                    "triggerBindings": [],
                }
            )
        )
        connections_file.chmod(0o600)
        local = await load_dex_web_project(connections_file)
        assert (await local.resolve_credential(ConnectionKey("sample", "main")))[
            "apiKey"
        ] == "REDACTED_LOCAL_KEY"

    s3_client = ExampleS3Client()
    s3_store = await S3ObjectStore.create(s3_client, "example-bucket", allow_unencrypted=True)
    await s3_store.create_object("projects/example/preview/configuration/head", b"{}")
    assert s3_client.put_arguments is not None and s3_client.put_arguments["IfNoneMatch"] == "*"

    project_connections = ConnectionStore(InMemoryObjectStore(), scope)
    key = ConnectionKey("sample", "main")
    await project_connections.replace_credential(
        key, 0, CredentialMaterial(
            {"access": "INERT_OLD_ACCESS", "refresh": "INERT_REFRESH"},
            datetime.now(UTC) - timedelta(minutes=1), "oauth2",
        ),
    )
    refreshing = RefreshingCredentialProvider(
        project_connections, key, lambda value: value,
        lambda value: value,
    )
    refreshed = await resolve_credential(refreshing, call, ExampleProjectRefreshDriver())
    assert refreshed["access"] == "INERT_NEW_ACCESS"
    assert (await project_connections.read_connection(key)).status == "READY"


if __name__ == "__main__":
    asyncio.run(main())
