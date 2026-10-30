# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Project snapshot, credential, local Dex Web, broker, and S3 tests."""

import json
from datetime import UTC, datetime, timedelta
from io import BytesIO
from pathlib import Path

import httpx
import pytest

from dex_connectors.sdk.credentials import ReauthorizationRequiredError
from dex_connectors.sdk.projectconfig import (
    Configuration,
    ConfigurationStore,
    ConnectionConfiguration,
    ConnectionKey,
    ConnectionStore,
    CredentialMaterial,
    EnvironmentValue,
    InMemoryObjectStore,
    Scope,
    load_dex_web_project,
    load_from_environment,
    load_project_from_object_store,
)
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerClient,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    RejectedCredentialNotRefreshedError,
    SharedConnection,
)
from dex_connectors.sdk.projectconfig.connections import (
    _connection_record_wire,
    _credential_envelope_wire,
)
from dex_connectors.sdk.projectconfig.models import (
    ApplicationSecretRef,
    CredentialBrokerConfiguration,
    digest_bytes,
    go_canonical_json_bytes,
    validate_secret_reference,
)
from dex_connectors.sdk.projectconfig.storage import (
    ObjectConflictError,
    OutcomeUnknownError,
    S3ObjectStore,
)
from dex_connectors.sdk.timestamps import RFC3339NanoTimestamp


@pytest.mark.asyncio
async def test_go_canonical_snapshot_and_credential_bytes(conformance_dir: Path) -> None:
    """Match Go's Unicode escaping, field order, digest, and local CAS identity."""
    fixture = json.loads((conformance_dir / "project-storage.json").read_text())
    configuration = Configuration.from_wire(fixture["configurationSnapshotDocument"])
    objects = InMemoryObjectStore()
    configuration_store = ConfigurationStore(objects, configuration.scope)
    _, written_snapshot = await configuration_store.write_configuration(0, configuration)
    snapshot_bytes = written_snapshot.contents
    assert snapshot_bytes.decode() == fixture["configurationSnapshotCanonicalJSON"]
    assert digest_bytes(snapshot_bytes) == fixture["snapshot"]["digest"]
    assert (await configuration_store.freeze_configuration(1)).to_wire() == fixture["snapshot"]
    connection_key = ConnectionKey(
        fixture["connectionKey"]["connectorId"], fixture["connectionKey"]["connectionName"]
    )
    assert connection_key.path(configuration.scope) + "/head" == fixture["connectionHeadKey"]
    credential_fixture = fixture["credentialEnvelope"]
    timestamp = RFC3339NanoTimestamp.from_wire(credential_fixture["expiresAt"])
    connection_store = ConnectionStore(objects, configuration.scope)
    await connection_store.replace_credential(
        connection_key,
        0,
        CredentialMaterial(credential_fixture["credentials"], timestamp, credential_fixture["authMethod"]),
    )
    credential_key = fixture["credentialObjectKey"]
    credential_bytes = (await objects.read_object(credential_key)).contents
    assert credential_bytes.decode() == fixture["credentialEnvelopeCanonicalJSON"]
    assert digest_bytes(credential_bytes)[7:] == fixture["objectReference"]["digest"]
    assert (await objects.read_object(credential_key)).key == fixture["objectReference"]["key"]
    loaded_credential, _ = await connection_store.read_credential_material(connection_key)
    assert str(loaded_credential.expires_at) == credential_fixture["expiresAt"]
    exchange_envelope = _credential_envelope_wire(
        configuration.scope,
        connection_key,
        2,
        credential_fixture["credentials"],
        timestamp,
        credential_fixture["authMethod"],
        "attempt-7",
    )
    assert (
        go_canonical_json_bytes(exchange_envelope).decode()
        == fixture["credentialExchangeEnvelopeCanonicalJSON"]
    )
    record_fixture = fixture["connectionRecord"]
    connection_record_bytes = go_canonical_json_bytes(
        _connection_record_wire(
            configuration.scope,
            connection_key,
            record_fixture["revision"],
            record_fixture["fence"],
            record_fixture["status"],
            timestamp,
            record_fixture["authMethod"],
            fixture["objectReference"],
            fixture["mutation"],
        )
    )
    assert connection_record_bytes.decode() == fixture["connectionRecordCanonicalJSON"]
    expected_etag = f"{digest_bytes(connection_record_bytes)[7:]}-{fixture['objectCAS']['version']}"
    assert expected_etag == fixture["objectCAS"]["etag"]
    assert fixture["objectCAS"]["key"] == fixture["connectionHeadKey"]
    cas_objects = InMemoryObjectStore()
    initial = await cas_objects.create_object(fixture["connectionHeadKey"], b"{}")
    observed_cas = await cas_objects.compare_and_swap_object(
        fixture["connectionHeadKey"], initial.etag, connection_record_bytes
    )
    assert observed_cas.etag == fixture["objectCAS"]["etag"]
    completed = fixture["completedConnectionRecord"]
    assert (
        _connection_record_wire(
            configuration.scope,
            connection_key,
            completed["revision"],
            completed["fence"],
            completed["status"],
            timestamp,
            completed["authMethod"],
            completed["credential"],
        )
        == completed
    )
    assert completed["credential"]["key"] == fixture["exchangeResultKey"]
    secret_reference = ApplicationSecretRef.from_wire(fixture["applicationSecret"])
    validate_secret_reference(secret_reference, configuration.scope, "DEX_CREDENTIAL_BROKER_TOKEN")
    assert fixture["emptyTriggerInboxDocument"] == {
        "schemaVersion": "connectors.dex.dev/project-trigger-inbox/v1alpha1",
        "events": [],
    }
    assert (
        json.loads(go_canonical_json_bytes(fixture["triggerInboxDocument"]))
        == fixture["triggerInboxDocument"]
    )
    assert fixture["triggerInboxHeadKey"].startswith(configuration.scope.prefix + "/trigger-inboxes/sample/")


@pytest.mark.asyncio
async def test_snapshot_cas_and_exact_version() -> None:
    """A frozen configuration survives later revisions and rejects wrong digests."""
    scope = Scope("project-1", "preview")
    objects = InMemoryObjectStore()
    store = ConfigurationStore(objects, scope)
    document = Configuration(scope, 1, (ConnectionConfiguration("sample", "main", "module", "sample", {}),))
    first, _ = await store.write_configuration(0, document)
    frozen = await store.freeze_configuration(first.revision)
    second, _ = await store.write_configuration(1, document)
    assert second.revision == 2
    assert (await store.read_snapshot(frozen)).revision == 1
    with pytest.raises(ObjectConflictError):
        await store.write_configuration(1, document)
    with pytest.raises(ValueError):
        await store.read_snapshot(type(frozen)(frozen.key, frozen.version, "sha256:" + "0" * 64))


@pytest.mark.asyncio
async def test_connection_private_material_and_revision() -> None:
    """Credential heads use CAS and immutable exact-version material."""
    objects = InMemoryObjectStore()
    scope = Scope("project-1", "live")
    store = ConnectionStore(objects, scope)
    key = ConnectionKey("sample", "main / west")
    first = await store.replace_credential(key, 0, CredentialMaterial({"apiKey": "inert"}, None, "apiKey"))
    assert first.revision == 1
    material, safe = await store.read_credential_material(key)
    assert material.credentials == {"apiKey": "inert"}
    assert "inert" not in repr(material) + repr(safe)
    with pytest.raises(ObjectConflictError):
        await store.replace_credential(key, 0, material)
    second = await store.replace_credential(key, 1, CredentialMaterial({"apiKey": "new"}, None, "apiKey"))
    assert second.fence == 2
    assert key.path(scope).endswith("/bWFpbiAvIHdlc3Q")


@pytest.mark.asyncio
async def test_connection_rejects_foreign_private_reference() -> None:
    """A tampered credential head cannot read material outside its key prefix."""
    objects = InMemoryObjectStore()
    scope = Scope("project-1", "preview")
    store = ConnectionStore(objects, scope)
    key = ConnectionKey("sample", "main")
    await store.replace_credential(key, 0, CredentialMaterial({"apiKey": "inert"}, None, "apiKey"))
    head = await objects.read_object(key.path(scope) + "/head")
    record = json.loads(head.contents)
    record["credential"]["key"] = "projects/other/preview/connections/sample/foreign/credentials/secret"
    await objects.compare_and_swap_object(head.key, head.etag, json.dumps(record).encode())
    with pytest.raises(ValueError):
        await store.read_credential_material(key)


@pytest.mark.asyncio
async def test_local_connections_file_schema_and_permissions(conformance_dir: Path, tmp_path: Path) -> None:
    """Load Go's Dex Web fixture and reject a loose private file mode."""
    source = conformance_dir / "connections.json"
    target = tmp_path / "connections.json"
    target.write_bytes(source.read_bytes())
    target.chmod(0o600)
    project = await load_dex_web_project(target)
    source_document = json.loads(source.read_text())
    assert len(project.configuration.connections) == len(source_document["connections"])
    assert {entry.connection_name for entry in project.configuration.connections} == {
        entry["connectionName"] for entry in source_document["connections"]
    }
    assert project.configuration.trigger_bindings == tuple(source_document["triggerBindings"])
    assert source_document["connections"][-1]["authMethodIds"] is None
    assert (await project.resolve_credential(ConnectionKey("sample", "élément <&> 😀")))[
        "apiKey"
    ] == "REDACTED_UNICODE_KEY"
    assert (await project.resolve_credential(ConnectionKey("sample", "api")))["apiKey"] == "REDACTED_API_KEY"
    updated = json.loads(target.read_text())
    updated["connections"][0]["credentials"]["apiKey"] = "REDACTED_NEW_KEY"
    target.write_text(json.dumps(updated))
    target.chmod(0o600)
    assert (await project.resolve_credential(ConnectionKey("sample", "api")))["apiKey"] == "REDACTED_NEW_KEY"
    target.chmod(0o644)
    with pytest.raises(ValueError):
        await load_dex_web_project(target)


@pytest.mark.asyncio
async def test_local_sidecar_and_hosted_selector_conflicts(tmp_path: Path) -> None:
    """Reject loose use-configurations files and mixed local/hosted selectors."""
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
    sidecar = tmp_path / "use-configurations.json"
    sidecar.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-use-configurations/v1alpha1",
                "operationConfigurations": [],
            }
        )
    )
    sidecar.chmod(0o644)
    with pytest.raises(ValueError):
        await load_dex_web_project(path)
    with pytest.raises(ValueError):
        await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(path), "DEX_PROJECT_ID": "other"})
    sidecar.chmod(0o600)
    assert (await load_dex_web_project(path)).configuration.connections == ()


@pytest.mark.asyncio
async def test_pinned_application_secret_and_reserved_names() -> None:
    """Resolve only exact secret versions within the accepted scope."""
    scope = Scope("project-1", "preview")
    objects = InMemoryObjectStore()
    store = ConfigurationStore(objects, scope)
    reference = await store.create_application_secret("WEBHOOK_SECRET", "inert<&>\u2028secret")
    document = Configuration(
        scope, 1, (), environment={"WEBHOOK_SECRET": EnvironmentValue(secret_ref=reference)}
    )
    written, _ = await store.write_configuration(0, document)
    snapshot = await store.freeze_configuration(written.revision)
    project = await load_project_from_object_store(objects, scope, snapshot)
    assert await project.resolve_application_environment() == {"WEBHOOK_SECRET": "inert<&>\u2028secret"}
    private_object = await objects.read_object(reference.key, reference.version)
    assert b"\\u003c\\u0026\\u003e\\u2028" in private_object.contents
    assert digest_bytes(private_object.contents) == reference.digest
    with pytest.raises(ValueError):
        await store.create_application_secret("DEX_CONFIG_KEY", "bad")
    with pytest.raises(ValueError):
        await store.read_secret_value(reference, "OTHER_SECRET")
    with pytest.raises(ValueError):
        Configuration(
            scope,
            1,
            (),
            environment={
                "WEBHOOK_SECRET": EnvironmentValue(
                    secret_ref=type(reference)(
                        reference.key.replace("project-1", "other"), reference.version, reference.digest
                    )
                )
            },
        ).validate()


def test_configuration_rejects_duplicate_operation_and_public_http_broker() -> None:
    """A document cannot bind the same Step twice or point its broker to public HTTP."""
    scope = Scope("project-1", "preview")
    entry = ConnectionConfiguration("sample", "main", "module", "sample", {})
    operation = {
        "connectorId": "sample",
        "connectionName": "main",
        "operationId": "getItem",
        "flowType": "ExampleFlow",
        "stepType": "GetItem",
        "configuration": {},
    }
    with pytest.raises(ValueError):
        Configuration(scope, 1, (entry,), operation_configurations=(operation, operation)).validate()
    token = ApplicationSecretRef(
        f"{scope.prefix}/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/" + "0" * 32,
        "version-1",
        "sha256:" + "0" * 64,
    )
    with pytest.raises(ValueError):
        Configuration(
            scope,
            1,
            (entry,),
            credential_broker=CredentialBrokerConfiguration("http://public.example/read", token),
        ).validate()


@pytest.mark.asyncio
async def test_broker_wire_renewal_and_revision(conformance_dir: Path) -> None:
    """Read Go broker fixture, renew a rotated token once, and reject same revision."""
    fixture = json.loads((conformance_dir / "credential-broker.json").read_text())
    requests: list[dict[str, object]] = []

    def broker_response(request: httpx.Request) -> httpx.Response:
        requests.append(json.loads(request.content))
        if request.headers["Authorization"] == "Bearer old":
            return httpx.Response(401)
        ready = dict(fixture["ready"])
        ready["expiresAt"] = (datetime.now(UTC) + timedelta(hours=1)).isoformat()
        return httpx.Response(200, json=ready)

    async def read_token(reference: ApplicationSecretRef) -> str:
        return "new" if reference.version == "new" else "old"

    async def current_reference() -> ApplicationSecretRef:
        return ApplicationSecretRef("token", "new", "sha256:" + "0" * 64)

    async with httpx.AsyncClient(
        transport=httpx.MockTransport(broker_response), follow_redirects=False
    ) as http_client:
        broker = CredentialBrokerClient(
            CredentialBrokerConfiguration(
                "https://broker.example/read", ApplicationSecretRef("token", "old", "sha256:" + "0" * 64)
            ),
            read_token,
            current_reference,
            http_client,
        )
        shared = SharedConnection("shared-7", broker)
        assert (await shared.resolve("call-1"))["accessToken"] == "REDACTED_ACCESS_TOKEN"
        assert (await broker.read("shared-7", 3)).revision == 4
        with pytest.raises(RejectedCredentialNotRefreshedError):
            await shared.resolve_after_rejection("call-1")
    assert requests[0] == fixture["firstRequest"]
    assert fixture["refreshRequest"] in requests
    assert requests[-1] == {"sharedConnectionId": "shared-7", "rejectedRevision": 4}
    ready_expiry = CredentialBrokerClient._decode(httpx.Response(200, json=fixture["ready"])).expires_at
    assert ready_expiry is not None and ready_expiry.utcoffset() == timedelta(hours=8)
    with pytest.raises(ReauthorizationRequiredError):
        CredentialBrokerClient._decode(httpx.Response(200, json=fixture["reauthorizationRequired"]))
    with pytest.raises(ConnectionNotSelectedError):
        CredentialBrokerClient._decode(httpx.Response(200, json=fixture["connectionNotSelected"]))
    assert fixture["refreshRequest"] == {"sharedConnectionId": "shared-7", "rejectedRevision": 3}


@pytest.mark.asyncio
async def test_go_project_environment_selectors(conformance_dir: Path) -> None:
    """Load hosted fixture settings and reject mixed or unsafe local overrides."""
    environment = json.loads((conformance_dir / "project-environment.json").read_text())
    storage = json.loads((conformance_dir / "project-storage.json").read_text())
    assert environment["credentialBrokerSecretName"] == "DEX_CREDENTIAL_BROKER_TOKEN"

    class FixtureS3:
        def get_bucket_versioning(self, **_: object) -> dict[str, str]:
            return {"Status": "Enabled"}

        def get_object(self, **_: object) -> dict[str, object]:
            return {
                "VersionId": storage["snapshot"]["version"],
                "ETag": '"fixture-etag"',
                "ServerSideEncryption": "aws:kms",
                "SSEKMSKeyId": environment["hosted"]["DEX_PROJECT_STORAGE_KMS_KEY_ARN"],
                "Body": BytesIO(storage["configurationSnapshotCanonicalJSON"].encode()),
            }

    project = await load_from_environment(environment["hosted"], FixtureS3())
    assert project.configuration.to_wire() == storage["configurationSnapshotDocument"]
    await project.close()
    with pytest.raises(ValueError):
        await load_from_environment({**environment["hosted"], **environment["localDexWeb"]})
    local_project = await load_from_environment(
        {**environment["hosted"], **environment["localObjectStoreOverrides"]}, FixtureS3()
    )
    assert local_project.configuration.scope == project.configuration.scope
    await local_project.close()
    with pytest.raises(ValueError):
        await load_from_environment(
            {
                **environment["hosted"],
                "DEX_PROJECT_ALLOW_LOCAL_STORAGE": "true",
                "DEX_PROJECT_STORAGE_ENDPOINT": "https://public.example",
            },
            FixtureS3(),
        )


@pytest.mark.asyncio
async def test_broker_status_classification() -> None:
    """Transient responses retry; malformed or rejected responses terminate."""

    async def token(_: ApplicationSecretRef) -> str:
        return "inert"

    async def reference() -> ApplicationSecretRef:
        return ApplicationSecretRef("token", "v1", "sha256:" + "0" * 64)

    config = CredentialBrokerConfiguration("https://broker.example/read", await reference())
    for status, error_type in ((503, CredentialBrokerUnavailableError), (400, CredentialBrokerRejectedError)):
        async with httpx.AsyncClient(
            transport=httpx.MockTransport(lambda _, status=status: httpx.Response(status))
        ) as client:
            broker = CredentialBrokerClient(config, token, reference, client)
            with pytest.raises(error_type):
                await broker.read("shared-7")


@pytest.mark.asyncio
async def test_broker_rejects_invalid_ready_and_oversized_body() -> None:
    """Malformed READY and oversized answers are terminal without exposing body text."""

    async def token(_: ApplicationSecretRef) -> str:
        return "inert"

    async def reference() -> ApplicationSecretRef:
        return ApplicationSecretRef("token", "v1", "sha256:" + "0" * 64)

    config = CredentialBrokerConfiguration("https://broker.example/read", await reference())
    answers = ({"status": "READY", "credential": {}, "revision": 0}, {"status": "UNKNOWN", "secret": "inert"})
    for answer in answers:
        async with httpx.AsyncClient(
            transport=httpx.MockTransport(lambda _, answer=answer: httpx.Response(200, json=answer))
        ) as client:
            with pytest.raises(CredentialBrokerRejectedError):
                await CredentialBrokerClient(config, token, reference, client).read("shared-7")
    oversized = b"x" * ((1 << 20) + 1)
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: httpx.Response(200, content=oversized))
    ) as client:
        with pytest.raises(CredentialBrokerRejectedError):
            await CredentialBrokerClient(config, token, reference, client).read("shared-7")


@pytest.mark.asyncio
async def test_broker_refused_unchanged_scope_token() -> None:
    """A broker 401 with no rotated token is a terminal rejection."""

    async def token(_: ApplicationSecretRef) -> str:
        return "inert"

    async def reference() -> ApplicationSecretRef:
        return ApplicationSecretRef("token", "v1", "sha256:" + "0" * 64)

    async with httpx.AsyncClient(transport=httpx.MockTransport(lambda _: httpx.Response(401))) as client:
        broker = CredentialBrokerClient(
            CredentialBrokerConfiguration("https://broker.example/read", await reference()),
            token,
            reference,
            client,
        )
        with pytest.raises(CredentialBrokerRejectedError):
            await broker.read("shared-7")


@pytest.mark.asyncio
async def test_s3_store_conditional_arguments_and_versioning() -> None:
    """S3 writes use exact preconditions with a versioned bucket."""

    class FakeS3:
        def __init__(self) -> None:
            self.put_arguments: dict[str, object] = {}

        def get_bucket_versioning(self, **_: object) -> dict[str, str]:
            return {"Status": "Enabled"}

        def put_object(self, **arguments: object) -> dict[str, str]:
            self.put_arguments = arguments
            return {"VersionId": "v1", "ETag": '"etag"'}

    client = FakeS3()
    store = await S3ObjectStore.create(
        client, "bucket", kms_key_arn="arn:aws:kms:us-west-2:123456789012:key/example-key"
    )
    await store.create_object("projects/p1/live/configuration/head", b"{}")
    assert client.put_arguments["IfNoneMatch"] == "*"
    assert client.put_arguments["SSEKMSKeyId"] == "arn:aws:kms:us-west-2:123456789012:key/example-key"
    await store.compare_and_swap_object("projects/p1/live/configuration/head", '"etag"', b'{"x":1}')
    assert client.put_arguments["IfMatch"] == '"etag"'


@pytest.mark.asyncio
async def test_s3_store_failure_classification_and_exact_read() -> None:
    """S3 rejects disabled versioning, ambiguous writes, and mismatched KMS reads."""

    class S3Error(Exception):
        response = {"ResponseMetadata": {"HTTPStatusCode": 412}}

    class FakeS3:
        versioning = "Enabled"
        fail_write = False

        def __init__(self) -> None:
            self.body = BytesIO(b"{}")

        def get_bucket_versioning(self, **_: object) -> dict[str, str]:
            return {"Status": self.versioning}

        def put_object(self, **_: object) -> dict[str, str]:
            if self.fail_write:
                raise S3Error()
            return {}

        def get_object(self, **_: object) -> dict[str, object]:
            return {
                "VersionId": "v1",
                "ETag": '"etag"',
                "ServerSideEncryption": "AES256",
                "Body": self.body,
            }

    fake = FakeS3()
    fake.versioning = "Suspended"
    with pytest.raises(ValueError):
        await S3ObjectStore.create(fake, "bucket", allow_unencrypted=True)
    fake.versioning = "Enabled"
    store = await S3ObjectStore.create(
        fake, "bucket", kms_key_arn="arn:aws:kms:us-west-2:123456789012:key/example-key"
    )
    with pytest.raises(OutcomeUnknownError):
        await store.create_object("projects/p1/live/head", b"{}")
    fake.fail_write = True
    with pytest.raises(ObjectConflictError):
        await store.create_object("projects/p1/live/head", b"{}")
    with pytest.raises(ValueError):
        await store.read_object("projects/p1/live/head", "v1")
    assert fake.body.closed
