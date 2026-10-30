# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Durable project credential refresh admission and recovery tests."""

import asyncio
from datetime import UTC, datetime, timedelta
from typing import Any

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
    ConnectionKey,
    ConnectionStore,
    CredentialMaterial,
    InMemoryObjectStore,
    Scope,
)
from dex_connectors.sdk.projectconfig.provider import (
    RefreshFailedError,
    RefreshingCredentialProvider,
    RejectedCredentialNotRefreshedError,
)

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
