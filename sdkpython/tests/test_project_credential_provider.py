# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Durable project credential refresh admission and recovery tests."""

import asyncio
import json
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

import httpx
import pytest

from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    CredentialRefreshResult,
    CredentialRefreshState,
    OperationRef,
    ReauthorizationRequiredError,
    resolve_credential,
    resolve_credential_after_rejection,
)
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
    load_dex_web_project,
    load_project_from_object_store,
)
from dex_connectors.sdk.projectconfig.broker import ConnectionNotSelectedError
from dex_connectors.sdk.projectconfig.connections import _connection_record_wire, _credential_envelope_wire
from dex_connectors.sdk.projectconfig.models import (
    CredentialBrokerConfiguration,
    go_canonical_json_bytes,
)
from dex_connectors.sdk.projectconfig.provider import (
    CredentialProvider,
    ExchangePendingError,
    RefreshFailedError,
    RefreshingCredentialProvider,
    RejectedCredentialNotRefreshedError,
)
from dex_connectors.sdk.projectconfig.storage import ObjectConflictError

KEY = ConnectionKey("sample", "main")
CALL = Call(
    CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf"),
    ConnectionRef("sample", "main"), OperationRef("sample", "createItem"),
)


def decode(value: dict[str, Any]) -> dict[str, str]:
    """Require complete private credential fields."""
    if not isinstance(value.get("access"), str) or not isinstance(value.get("refresh"), str):
        raise ValueError("credential is incomplete")
    return {"access": value["access"], "refresh": value["refresh"]}


def encode(value: dict[str, str]) -> dict[str, Any]:
    """Persist complete replacement material."""
    return value.copy()


class RefreshDriver:
    """Observe exactly one admitted provider exchange across two replicas."""

    def __init__(self) -> None:
        self.calls = 0
        self.started = asyncio.Event()
        self.release = asyncio.Event()
        self.error: Exception | None = None

    def refresh_required(self, state: CredentialRefreshState[dict[str, str]]) -> bool:
        """Refresh only an expired token."""
        return state.expires_at is not None and state.expires_at <= state.now

    async def refresh(
        self, state: CredentialRefreshState[dict[str, str]]
    ) -> CredentialRefreshResult[dict[str, str]]:
        """Hold one provider request until the test releases it."""
        self.calls += 1
        self.started.set()
        await self.release.wait()
        if self.error is not None:
            raise self.error
        return CredentialRefreshResult(
            {"access": "new", "refresh": state.credentials["refresh"]},
            datetime.now(UTC) + timedelta(hours=1),
        )


@pytest.mark.asyncio
async def test_refresh_admission_across_replicas() -> None:
    """Only the CAS winner dispatches; an observer reads the published result."""
    objects = InMemoryObjectStore()
    scope = Scope("test-project", "preview")
    first_store = ConnectionStore(objects, scope)
    second_store = ConnectionStore(objects, scope)
    await first_store.replace_credential(
        KEY, 0, CredentialMaterial(
            {"access": "old", "refresh": "kept"}, datetime.now(UTC) - timedelta(minutes=1), "oauth2"
        ),
    )
    first = RefreshingCredentialProvider(first_store, KEY, decode, encode)
    second = RefreshingCredentialProvider(second_store, KEY, decode, encode)
    driver = RefreshDriver()
    first_task = asyncio.create_task(resolve_credential(first, CALL, driver))
    await asyncio.wait_for(driver.started.wait(), 2)
    second_task = asyncio.create_task(resolve_credential(second, CALL, driver))
    driver.release.set()
    resolved = await asyncio.wait_for(asyncio.gather(first_task, second_task), 3)
    assert len(resolved) == 2
    assert all(value == {"access": "new", "refresh": "kept"} for value in resolved)
    assert driver.calls == 1
    assert (await first_store.read_connection(KEY)).status == "READY"
    assert (await first_store.read_credential_material(KEY))[0].credentials["refresh"] == "kept"


async def _authorize_record(store: ConnectionStore, deadline: datetime) -> str:
    """Seed a valid admitted OAuth authorization without calling a provider."""
    path = KEY.path(store.scope)
    result_key = f"{path}/exchange-results/authorization-1"
    mutation: dict[str, object] = {
        "kind": "AUTHORIZING", "invocationId": "invocation-1", "id": "authorization-1",
        "fence": 1, "baseRevision": 0, "deadline": deadline.isoformat(), "resultKey": result_key,
    }
    record = _connection_record_wire(
        store.scope, KEY, 1, 1, "AUTHORIZING", None, "oauth2", None, mutation
    )
    await store.objects.create_object(f"{path}/head", go_canonical_json_bytes(record))
    return result_key


@pytest.mark.asyncio
async def test_expired_authorization_recovers_result_or_fences_reauthorization() -> None:
    """An abandoned OAuth admission publishes its result or requires reauthorization."""
    scope = Scope("test-project", "preview")
    pending_store = ConnectionStore(InMemoryObjectStore(), scope)
    await _authorize_record(pending_store, datetime.now(UTC) + timedelta(minutes=1))
    pending_provider = RefreshingCredentialProvider(pending_store, KEY, decode, encode)
    with pytest.raises(ExchangePendingError):
        await resolve_credential(pending_provider, CALL, RefreshDriver())

    expired_store = ConnectionStore(InMemoryObjectStore(), scope)
    await _authorize_record(expired_store, datetime.now(UTC) - timedelta(minutes=1))
    expired_provider = RefreshingCredentialProvider(expired_store, KEY, decode, encode)
    with pytest.raises(ReauthorizationRequiredError):
        await resolve_credential(expired_provider, CALL, RefreshDriver())
    assert (await expired_store.read_connection(KEY)).status == "REAUTHORIZATION_REQUIRED"

    completed_store = ConnectionStore(InMemoryObjectStore(), scope)
    result_key = await _authorize_record(completed_store, datetime.now(UTC) - timedelta(minutes=1))
    envelope = _credential_envelope_wire(
        scope, KEY, 1, {"access": "authorized", "refresh": "kept"},
        datetime.now(UTC) + timedelta(hours=1), "oauth2", "authorization-1",
    )
    await completed_store.objects.create_object(result_key, go_canonical_json_bytes(envelope))
    completed_provider = RefreshingCredentialProvider(completed_store, KEY, decode, encode)
    assert (await resolve_credential(completed_provider, CALL, RefreshDriver()))["access"] == "authorized"
    assert (await completed_store.read_connection(KEY)).status == "READY"


@pytest.mark.asyncio
async def test_expired_refresh_observers_reload_after_recovery_conflict(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Both observers resolve current material when one restores the stale admission."""
    objects = InMemoryObjectStore()
    scope = Scope("test-project", "preview")
    first_store = ConnectionStore(objects, scope)
    second_store = ConnectionStore(objects, scope)
    await first_store.replace_credential(
        KEY, 0, CredentialMaterial(
            {"access": "current", "refresh": "kept"}, datetime.now(UTC) + timedelta(hours=1),
            "oauth2",
        ),
    )
    first = RefreshingCredentialProvider(first_store, KEY, decode, encode)
    second = RefreshingCredentialProvider(second_store, KEY, decode, encode)
    record, _ = await first_store._read_record(KEY)
    await first._admit_refresh(record, datetime.now(UTC) - timedelta(minutes=2))
    second_observing = asyncio.Event()
    release_second = asyncio.Event()
    original_recover = second._recover_or_wait

    async def recover_after_first(record: dict[str, Any]) -> None:
        second_observing.set()
        await release_second.wait()
        await original_recover(record)

    monkeypatch.setattr(second, "_recover_or_wait", recover_after_first)
    driver = RefreshDriver()
    second_task = asyncio.create_task(resolve_credential(second, CALL, driver))
    await asyncio.wait_for(second_observing.wait(), 2)
    first_result = await asyncio.wait_for(resolve_credential(first, CALL, driver), 2)
    release_second.set()
    second_result = await asyncio.wait_for(second_task, 2)
    assert first_result == second_result == {"access": "current", "refresh": "kept"}
    assert driver.calls == 0


@pytest.mark.asyncio
async def test_refresh_failure_restores_prior_and_terminal_reauth_fences() -> None:
    """Retryable failure retains old material; terminal grant failure fences it."""
    objects = InMemoryObjectStore()
    store = ConnectionStore(objects, Scope("test-project", "preview"))
    await store.replace_credential(
        KEY, 0, CredentialMaterial(
            {"access": "old", "refresh": "kept"}, datetime.now(UTC) - timedelta(minutes=1), "oauth2"
        ),
    )
    provider = RefreshingCredentialProvider(store, KEY, decode, encode)
    retryable = RefreshDriver()
    retryable.error = OSError("inert provider detail")
    retryable.release.set()
    with pytest.raises(RefreshFailedError) as failed:
        await resolve_credential(provider, CALL, retryable)
    assert "inert provider detail" not in str(failed.value)
    assert (await store.read_connection(KEY)).status == "READY"
    terminal = RefreshDriver()
    terminal.error = ReauthorizationRequiredError()
    terminal.release.set()
    with pytest.raises(ReauthorizationRequiredError):
        await resolve_credential(provider, CALL, terminal)
    assert (await store.read_connection(KEY)).status == "REAUTHORIZATION_REQUIRED"
    with pytest.raises(ReauthorizationRequiredError):
        await resolve_credential(provider, CALL, terminal)
    assert terminal.calls == 1


@pytest.mark.asyncio
async def test_rejection_requires_authoritative_expiry() -> None:
    """A generic provider 401 never rotates a still-valid OAuth credential."""
    objects = InMemoryObjectStore()
    store = ConnectionStore(objects, Scope("test-project", "preview"))
    await store.replace_credential(
        KEY, 0, CredentialMaterial(
            {"access": "valid", "refresh": "kept"}, datetime.now(UTC) + timedelta(hours=1), "oauth2"
        ),
    )
    provider = RefreshingCredentialProvider(store, KEY, decode, encode)
    driver = RefreshDriver()
    with pytest.raises(RejectedCredentialNotRefreshedError):
        await resolve_credential_after_rejection(provider, CALL, driver)
    assert driver.calls == 0


def _write_local_connections(path: Path, access: str, expires_at: datetime) -> None:
    """Write one owner-only Dex Web connection without exposing real credentials."""
    path.write_text(json.dumps({
        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
        "connections": [{
            "connectorId": "sample", "connectionName": "main", "modulePath": "example",
            "provider": "sample", "configuration": {},
            "credentials": {"access": access, "refresh": "kept"},
            "credentialExpiresAt": expires_at.isoformat(),
        }],
        "triggerBindings": [],
    }))
    path.chmod(0o600)


@pytest.mark.asyncio
async def test_local_dex_web_refresh_writes_back_and_restart_reads_it(tmp_path: Path) -> None:
    """Persist a rotated access token in Dex Web's owner-only file."""
    path = tmp_path / "connections.json"
    _write_local_connections(path, "old", datetime.now(UTC) - timedelta(minutes=1))
    project = await load_dex_web_project(path)
    provider = RefreshingCredentialProvider(project.connections, KEY, decode, encode)
    driver = RefreshDriver()
    driver.release.set()
    assert (await resolve_credential(provider, CALL, driver))["access"] == "new"
    assert json.loads(path.read_text())["connections"][0]["credentials"] == {
        "access": "new", "refresh": "kept"
    }
    assert path.stat().st_mode & 0o777 == 0o600
    restarted = await load_dex_web_project(path)
    material, _ = await restarted.connections.read_credential_material(KEY)
    assert material.credentials["access"] == "new"


@pytest.mark.asyncio
async def test_local_dex_web_newer_authorization_wins_refresh(tmp_path: Path) -> None:
    """A Dex Web update supersedes an in-flight refresh admission."""
    path = tmp_path / "connections.json"
    _write_local_connections(path, "old", datetime.now(UTC) - timedelta(minutes=1))
    project = await load_dex_web_project(path)
    provider = RefreshingCredentialProvider(project.connections, KEY, decode, encode)
    driver = RefreshDriver()
    pending = asyncio.create_task(resolve_credential(provider, CALL, driver))
    await asyncio.wait_for(driver.started.wait(), 2)
    _write_local_connections(path, "dex-web-new", datetime.now(UTC) + timedelta(hours=1))
    driver.release.set()
    with pytest.raises(ObjectConflictError):
        await pending
    assert json.loads(path.read_text())["connections"][0]["credentials"]["access"] == "dex-web-new"
    current, _ = await project.connections.read_credential_material(KEY)
    assert current.credentials["access"] == "dex-web-new"


class ForbiddenRefreshDriver:
    """Fail if a shared broker credential enters the provider refresh path."""

    def refresh_required(self, state: CredentialRefreshState[dict[str, str]]) -> bool:
        """A shared credential must never consult this driver."""
        raise AssertionError("shared credential consulted refresh driver")

    async def refresh(
        self, state: CredentialRefreshState[dict[str, str]]
    ) -> CredentialRefreshResult[dict[str, str]]:
        """A shared credential must never dispatch provider refresh."""
        raise AssertionError("shared credential dispatched refresh")


def decode_shared(value: dict[str, Any]) -> dict[str, str]:
    """Accept the access token a broker serves without host-kept renewal material."""
    if not isinstance(value.get("access"), str):
        raise ValueError("shared credential is incomplete")
    return {"access": value["access"]}


async def _load_shared_project(
    client: httpx.AsyncClient,
) -> tuple[ConnectionStore, LoadedProject]:
    """Load one ordinary and one broker-backed entry from a pinned document."""
    scope = Scope("test-project", "preview")
    objects = InMemoryObjectStore()
    configurations = ConfigurationStore(objects, scope)
    token_reference = await configurations.create_credential_broker_token("scope-token")
    configuration = Configuration(
        scope, 1,
        (
            ConnectionConfiguration("sample", "main", "example", "sample", {}),
            ConnectionConfiguration(
                "sample", "shared", "example", "sample", {}, shared_connection_id="shared-7"
            ),
        ),
        credential_broker=CredentialBrokerConfiguration("https://broker.example/read", token_reference),
    )
    written, _ = await configurations.write_configuration(0, configuration)
    snapshot = await configurations.freeze_configuration(written.revision)
    project = await load_project_from_object_store(objects, scope, snapshot, client)
    return project.connections, project


@pytest.mark.asyncio
async def test_shared_provider_routes_broker_without_refresh_or_stored_head() -> None:
    """Generated-style providers use broker bytes and the shared decoder only."""
    requests: list[dict[str, object]] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(json.loads(request.content))
        return httpx.Response(200, json={
            "status": "READY", "credential": {"access": f"shared-{len(requests)}"},
            "revision": len(requests),
            "expiresAt": (datetime.now(UTC) + timedelta(hours=1)).isoformat(),
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        store, project = await _load_shared_project(client)
        await store.replace_credential(
            KEY, 0, CredentialMaterial({"access": "ordinary", "refresh": "kept"}, None, "oauth2")
        )
        ordinary = CredentialProvider(store, KEY, decode)
        assert (await ordinary.resolve(CALL))["access"] == "ordinary"
        shared_key = ConnectionKey("sample", "shared")
        shared_call = Call(CALL.id, ConnectionRef("sample", "shared"), CALL.operation)
        refreshing = RefreshingCredentialProvider(store, shared_key, decode, encode, decode_shared)
        driver = ForbiddenRefreshDriver()
        assert (await resolve_credential(refreshing, shared_call, driver))["access"] == "shared-1"
        assert (await resolve_credential_after_rejection(
            refreshing, shared_call, driver
        ))["access"] == "shared-2"
        assert requests == [
            {"sharedConnectionId": "shared-7"},
            {"sharedConnectionId": "shared-7", "rejectedRevision": 1},
        ]
        assert (await CredentialProvider(store, shared_key, decode, decode_shared).resolve(
            shared_call
        ))["access"] == "shared-2"
        with pytest.raises(ValueError, match="shared project credential decoding failed"):
            await CredentialProvider(store, shared_key, decode).resolve(shared_call)
        await project.close()


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "expected_error"),
    [
        ("REAUTHORIZATION_REQUIRED", ReauthorizationRequiredError),
        ("CONNECTION_NOT_SELECTED", ConnectionNotSelectedError),
    ],
)
async def test_shared_provider_propagates_broker_status(
    status: str, expected_error: type[Exception]
) -> None:
    """Terminal broker decisions are surfaced without attempting a local refresh."""
    def respond(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"status": status})

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        store, project = await _load_shared_project(client)
        shared_call = Call(CALL.id, ConnectionRef("sample", "shared"), CALL.operation)
        provider = RefreshingCredentialProvider(
            store, ConnectionKey("sample", "shared"), decode, encode, decode_shared
        )
        with pytest.raises(expected_error):
            await resolve_credential(provider, shared_call, ForbiddenRefreshDriver())
        await project.close()
