# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed project credential providers with durable refresh admission."""

import asyncio
import json
import secrets
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, Generic, TypeVar

from ..credentials import CredentialRefreshDriver, CredentialRefreshState, ReauthorizationRequiredError
from ..timestamps import RFC3339NanoTimestamp, format_rfc3339_nano
from ..types import Call
from .broker import RejectedCredentialNotRefreshedError, SharedConnection
from .connections import (
    CONNECTION_SCHEMA_VERSION,
    ConnectionKey,
    ConnectionStore,
    CredentialMaterial,
    _connection_record_wire,
    _credential_envelope_wire,
)
from .models import digest_bytes, go_canonical_json_bytes, require_object
from .storage import Object, ObjectConflictError, ObjectNotFoundError, OutcomeUnknownError

Credential = TypeVar("Credential")
CredentialDecoder = Callable[[dict[str, Any]], Credential]
CredentialEncoder = Callable[[Credential], dict[str, Any]]
_REFRESH_TIMEOUT = timedelta(seconds=30)
_ADMISSION_TIMEOUT = timedelta(seconds=40)


class RefreshFailedError(OSError):
    """Refresh failed without proving the prior credential invalid."""


class ExchangePendingError(OSError):
    """Another invocation owns provider dispatch for this credential generation."""


@dataclass(frozen=True, slots=True)
class _Admission:
    key: ConnectionKey
    kind: str
    attempt_id: str
    fence: int
    deadline: datetime
    result_key: str
    can_dispatch: bool


class CredentialProvider(Generic[Credential]):
    """Resolve a non-refreshing project credential for one connector connection.

    The decoder validates private material at the connector boundary. Ordinary
    entries read current storage on every call. Shared entries use the loaded
    project's broker and optional shared decoder, without persisting credentials
    in Dex Flow state.
    """

    def __init__(
        self, store: ConnectionStore, key: ConnectionKey,
        decode: CredentialDecoder[Credential],
        decode_shared: CredentialDecoder[Credential] | None = None,
    ) -> None:
        if store is None or decode is None:
            raise ValueError("project credential store and decoder are required")
        key.path(store.scope)
        self._store = store
        self._key = key
        self._decode = decode
        self._decode_shared = decode_shared
        self._shared: SharedConnection | None = store._shared_connection(key)

    async def resolve(self, call: Call) -> Credential:
        """Read current material; an expired credential requires refresh support."""
        self._validate_call(call)
        if self._shared is not None:
            return self._decode_shared_material(await self._shared.resolve(str(call.id)))
        material, connection = await self._store.read_credential_material(self._key)
        if connection.expires_at is not None and connection.expires_at <= datetime.now(UTC):
            raise ValueError("expired project credential requires refresh")
        return self._decode_material(material)

    def _validate_call(self, call: Call) -> None:
        if (
            call.operation.connector_id != self._key.connector_id
            or call.connection.name != self._key.connection_name
        ):
            raise ValueError("connector call differs from configured project connection")
        call.operation.validate()

    def _decode_material(self, material: CredentialMaterial) -> Credential:
        try:
            return self._decode(material.credentials)
        except (ValueError, KeyError, TypeError):
            raise ValueError("project credential decoding failed") from None

    def _decode_shared_material(self, material: dict[str, object]) -> Credential:
        decoder = self._decode_shared or self._decode
        try:
            return decoder(material)
        except (ValueError, KeyError, TypeError):
            raise ValueError("shared project credential decoding failed") from None


class RefreshingCredentialProvider(CredentialProvider[Credential]):
    """Refresh project credentials once per generation across replicas.

    For ordinary entries, a conditional head write grants one caller provider
    dispatch. Observers recover the immutable result or restore an abandoned
    admission. Shared entries use the broker without consulting the driver.
    """

    def __init__(
        self, store: ConnectionStore, key: ConnectionKey,
        decode: CredentialDecoder[Credential], encode: CredentialEncoder[Credential],
        decode_shared: CredentialDecoder[Credential] | None = None,
    ) -> None:
        super().__init__(store, key, decode, decode_shared)
        if encode is None:
            raise ValueError("project credential encoder is required")
        self._encode = encode

    async def resolve_with_refresh(
        self, call: Call, driver: CredentialRefreshDriver[Credential]
    ) -> Credential:
        """Refresh expired or driver-stale material after winning durable CAS."""
        self._validate_call(call)
        if driver is None:
            raise ValueError("credential refresh driver is required")
        if self._shared is not None:
            return self._decode_shared_material(await self._shared.resolve(str(call.id)))
        while True:
            record, _ = await self._store._read_record(self._key)
            status = record["status"]
            if status in ("REVOKED", "REAUTHORIZATION_REQUIRED"):
                raise ReauthorizationRequiredError()
            if status == "AUTHORIZING":
                mutation = require_object(record.get("mutation"), "credential mutation")
                deadline = _as_datetime(_parse_expiry(mutation["deadline"]))
                if deadline is not None and datetime.now(UTC) < deadline:
                    raise ExchangePendingError("credential authorization exchange is pending")
                try:
                    await self._recover_or_wait(record)
                except ObjectConflictError:
                    continue
                continue
            if status == "REFRESHING":
                try:
                    await self._recover_or_wait(record)
                except ObjectConflictError:
                    continue
                continue
            if status != "READY":
                raise ValueError("project credential status is invalid")
            material = await self._read_material(record)
            credentials = self._decode_material(material)
            now = datetime.now(UTC)
            expires_at = _as_datetime(material.expires_at)
            state = CredentialRefreshState(credentials, expires_at, now)
            if (expires_at is None or expires_at > now) and not driver.refresh_required(state):
                return credentials
            try:
                admission = await self._admit_refresh(record, now)
            except ObjectConflictError:
                continue
            return await self._refresh_admitted(admission, material, state, driver)

    async def resolve_after_rejection(
        self, call: Call, driver: CredentialRefreshDriver[Credential]
    ) -> Credential:
        """Refresh after provider rejection only when stored expiry has elapsed."""
        self._validate_call(call)
        if driver is None:
            raise ValueError("credential refresh driver is required")
        if self._shared is not None:
            return self._decode_shared_material(
                await self._shared.resolve_after_rejection(str(call.id))
            )
        connection = await self._store.read_connection(self._key)
        if connection.expires_at is None or connection.expires_at > datetime.now(UTC):
            raise RejectedCredentialNotRefreshedError(
                "authentication rejection does not prove credential expiry"
            )
        return await self.resolve_with_refresh(call, driver)

    async def _read_material(self, record: dict[str, Any]) -> CredentialMaterial:
        reference = require_object(record.get("credential"), "credential reference")
        item = await self._store.objects.read_object(str(reference["key"]), str(reference["version"]))
        if digest_bytes(item.contents)[7:] != reference["digest"]:
            raise ValueError("project credential integrity differs")
        envelope = require_object(json.loads(item.contents), "credential envelope")
        if (
            envelope.get("schemaVersion") != CONNECTION_SCHEMA_VERSION
            or envelope.get("scope") != self._store.scope.to_wire()
            or envelope.get("connection") != self._key.to_wire()
            or envelope.get("authMethod") != record.get("authMethod")
            or envelope.get("expiresAt") != record.get("expiresAt")
        ):
            raise ValueError("project credential identity differs")
        return CredentialMaterial(
            require_object(envelope.get("credentials"), "credentials"),
            _parse_expiry(envelope.get("expiresAt")), str(envelope["authMethod"]),
        )

    async def _admit_refresh(self, record: dict[str, Any], now: datetime) -> _Admission:
        attempt_id = secrets.token_hex(16)
        path = self._key.path(self._store.scope)
        deadline = now + _ADMISSION_TIMEOUT
        mutation: dict[str, object] = {
            "kind": "REFRESHING", "invocationId": secrets.token_hex(16), "id": attempt_id,
            "fence": record["fence"] + 1, "baseRevision": record["revision"],
            "deadline": format_rfc3339_nano(deadline),
            "resultKey": f"{path}/exchange-results/{attempt_id}",
        }
        updated = _connection_record_wire(
            self._store.scope, self._key, record["revision"] + 1, record["fence"] + 1,
            "REFRESHING", _parse_expiry(record.get("expiresAt")), record["authMethod"],
            require_object(record["credential"], "credential reference"), mutation,
        )
        await self._write_head(record, updated)
        return _Admission(self._key, "REFRESHING", attempt_id, record["fence"] + 1, deadline,
                          str(mutation["resultKey"]), True)

    async def _refresh_admitted(
        self, admission: _Admission, prior: CredentialMaterial,
        state: CredentialRefreshState[Credential], driver: CredentialRefreshDriver[Credential],
    ) -> Credential:
        try:
            result = await asyncio.wait_for(driver.refresh(state), _REFRESH_TIMEOUT.total_seconds())
        except ReauthorizationRequiredError:
            await self._finish_exchange(admission, "REAUTHORIZATION_REQUIRED")
            raise
        except Exception:
            await self._finish_exchange(admission, "READY")
            raise RefreshFailedError(
                "project credential refresh failed; current authorization is kept"
            ) from None
        if result.expires_at <= datetime.now(UTC):
            await self._finish_exchange(admission, "READY")
            raise RefreshFailedError("refreshed credential has no future expiry")
        try:
            encoded = self._encode(result.credentials)
            if not isinstance(encoded, dict) or not encoded:
                raise ValueError("invalid private credential material")
        except (ValueError, TypeError):
            await self._finish_exchange(admission, "READY")
            raise RefreshFailedError("refreshed credential encoding failed") from None
        await self._commit_refresh(
            admission, CredentialMaterial(encoded, result.expires_at, prior.auth_method)
        )
        return result.credentials

    async def _recover_or_wait(self, record: dict[str, Any]) -> None:
        mutation = require_object(record.get("mutation"), "credential mutation")
        admission = _Admission(
            self._key, str(record["status"]), str(mutation["id"]), record["fence"],
            _as_datetime(_parse_expiry(mutation["deadline"])) or datetime.min.replace(tzinfo=UTC),
            str(mutation["resultKey"]), False,
        )
        try:
            result = await self._store.objects.read_object(admission.result_key)
        except ObjectNotFoundError:
            if datetime.now(UTC) < admission.deadline:
                await asyncio.sleep(0.1)
                return
            await self._finish_exchange(
                admission,
                "REAUTHORIZATION_REQUIRED" if admission.kind == "AUTHORIZING" else "READY",
            )
            return
        await self._publish_result(admission, result)

    async def _commit_refresh(self, admission: _Admission, material: CredentialMaterial) -> None:
        envelope = _credential_envelope_wire(
            self._store.scope, self._key, admission.fence, material.credentials,
            material.expires_at, material.auth_method, admission.attempt_id,
        )
        contents = go_canonical_json_bytes(envelope)
        try:
            result = await self._store.objects.create_object(admission.result_key, contents)
        except (ObjectConflictError, OutcomeUnknownError):
            result = await self._store.objects.read_object(admission.result_key)
            if result.contents != contents:
                raise ValueError("immutable credential result differs") from None
        await self._publish_result(admission, result)

    async def _publish_result(self, admission: _Admission, result: Object) -> None:
        record, _ = await self._store._read_record(self._key)
        if record["status"] == "READY" and record.get("credential", {}).get("key") == result.key:
            return
        _require_admission(record, admission)
        envelope = require_object(json.loads(result.contents), "credential result")
        if (
            envelope.get("schemaVersion") != CONNECTION_SCHEMA_VERSION
            or envelope.get("scope") != self._store.scope.to_wire()
            or envelope.get("connection") != self._key.to_wire()
            or envelope.get("fence") != admission.fence
            or envelope.get("attemptId") != admission.attempt_id
        ):
            raise ValueError("credential exchange result identity is invalid")
        reference = {
            "key": result.key, "version": result.version,
            "digest": digest_bytes(result.contents)[7:],
        }
        updated = _connection_record_wire(
            self._store.scope, self._key, record["revision"] + 1, record["fence"],
            "READY", _parse_expiry(envelope.get("expiresAt")), str(envelope["authMethod"]),
            reference,
        )
        try:
            await self._write_head(record, updated)
        except ObjectConflictError:
            observed, _ = await self._store._read_record(self._key)
            if observed["status"] != "READY" or observed.get("credential", {}).get("key") != result.key:
                raise

    async def _finish_exchange(self, admission: _Admission, status: str) -> None:
        record, _ = await self._store._read_record(self._key)
        _require_admission(record, admission)
        updated = _connection_record_wire(
            self._store.scope, self._key, record["revision"] + 1, record["fence"] + 1,
            status, _parse_expiry(record.get("expiresAt")), str(record["authMethod"]),
            require_object(record["credential"], "credential reference")
            if record.get("credential") is not None else None,
        )
        await self._write_head(record, updated)

    async def _write_head(self, prior: dict[str, Any], updated: dict[str, object]) -> None:
        head_path = f"{self._key.path(self._store.scope)}/head"
        _, head = await self._store._read_record(self._key)
        if require_object(json.loads(head.contents), "connection head") != prior:
            raise ObjectConflictError("connection revision conflict")
        contents = go_canonical_json_bytes(updated)
        try:
            await self._store.objects.compare_and_swap_object(head_path, head.etag, contents)
        except OutcomeUnknownError:
            observed = await self._store.objects.read_object(head_path)
            if observed.contents != contents:
                raise


def _require_admission(record: dict[str, Any], admission: _Admission) -> None:
    mutation = record.get("mutation")
    if (
        record["status"] != admission.kind or record["fence"] != admission.fence
        or not isinstance(mutation, dict) or mutation.get("id") != admission.attempt_id
        or mutation.get("kind") != admission.kind
    ):
        raise ObjectConflictError("credential exchange admission was superseded")


def _parse_expiry(value: object) -> datetime | None:
    if value is None:
        return None
    if isinstance(value, datetime):
        return value
    if not isinstance(value, str):
        raise ValueError("invalid credential expiry")
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def _as_datetime(value: object) -> datetime | None:
    if value is None:
        return None
    if isinstance(value, datetime):
        return value
    if isinstance(value, RFC3339NanoTimestamp):
        return value.parsed
    raise ValueError("invalid credential expiry")


__all__ = [
    "CredentialProvider", "ExchangePendingError", "RefreshFailedError",
    "RefreshingCredentialProvider", "RejectedCredentialNotRefreshedError",
]
