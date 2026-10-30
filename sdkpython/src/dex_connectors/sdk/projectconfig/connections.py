# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Versioned ordinary credential heads and immutable private material."""

import base64
import json
import re
from dataclasses import dataclass
from datetime import datetime
from typing import Any

from .models import Scope, digest_bytes, json_bytes, require_object
from .storage import Object, ObjectConflictError, ObjectNotFoundError, ObjectStore, OutcomeUnknownError

CONNECTION_SCHEMA_VERSION = "connectors.dex.dev/project-connection/v1alpha2"
_CONNECTOR_ID = re.compile(r"^[a-z][a-z0-9-]{1,62}$")


@dataclass(frozen=True, slots=True)
class ConnectionKey:
    """Manifest connector ID and application-declared connection name."""

    connector_id: str
    connection_name: str

    def to_wire(self) -> dict[str, str]:
        """Encode the stable connection key."""
        return {"connectorId": self.connector_id, "connectionName": self.connection_name}

    def path(self, scope: Scope) -> str:
        """Return the scoped head prefix after validating key identity."""
        if (
            not _CONNECTOR_ID.fullmatch(self.connector_id)
            or not self.connection_name
            or self.connection_name.strip() != self.connection_name
            or len(self.connection_name) > 256
            or any(character in self.connection_name for character in "\x00\r\n")
        ):
            raise ValueError("invalid project connection key")
        encoded = base64.urlsafe_b64encode(self.connection_name.encode()).decode().rstrip("=")
        return f"{scope.prefix}/connections/{self.connector_id}/{encoded}"


@dataclass(frozen=True, slots=True)
class CredentialMaterial:
    """Private connector credential object; never serialize into Flow state."""

    credentials: dict[str, Any]
    expires_at: datetime | None
    auth_method: str

    def __repr__(self) -> str:
        """Redact private credentials in debugging output."""
        return "CredentialMaterial([REDACTED])"

    def __reduce__(self) -> tuple[Any, ...]:
        """Reject accidental credential serialization."""
        raise TypeError("credential material cannot be serialized")


@dataclass(frozen=True, slots=True)
class Connection:
    """Safe authoritative connection metadata without credential bytes."""

    key: ConnectionKey
    scope: Scope
    revision: int
    fence: int
    status: str
    expires_at: datetime | None
    auth_method: str


class ConnectionStore:
    """Read exact material and conditionally replace connection credentials."""

    def __init__(self, objects: ObjectStore, scope: Scope) -> None:
        if objects is None:
            raise ValueError("project connection object store is required")
        self.objects = objects
        self.scope = scope

    async def read_connection(self, key: ConnectionKey) -> Connection:
        """Read safe metadata without refreshing credentials."""
        record, _ = await self._read_record(key)
        return self._connection(key, record)

    async def read_credential_material(self, key: ConnectionKey) -> tuple[CredentialMaterial, Connection]:
        """Read exact private material for trusted provider boundaries."""
        record, _ = await self._read_record(key)
        connection = self._connection(key, record)
        if connection.status != "READY":
            raise ValueError("project credential is unavailable")
        reference = require_object(record.get("credential"), "credential reference")
        item = await self.objects.read_object(str(reference["key"]), str(reference["version"]))
        if digest_bytes(item.contents)[7:] != reference["digest"]:
            raise ValueError("project credential integrity differs")
        envelope = require_object(json.loads(item.contents), "credential envelope")
        if (
            envelope.get("schemaVersion") != CONNECTION_SCHEMA_VERSION
            or envelope.get("scope") != self.scope.to_wire()
            or envelope.get("connection") != key.to_wire()
            or envelope.get("fence") != connection.fence
            or envelope.get("authMethod") != connection.auth_method
            or envelope.get("expiresAt") != record.get("expiresAt")
        ):
            raise ValueError("project credential identity differs")
        credentials = require_object(envelope.get("credentials"), "credentials")
        return CredentialMaterial(
            credentials, self._parse_time(envelope.get("expiresAt")), connection.auth_method
        ), connection

    async def replace_credential(
        self, key: ConnectionKey, expected_revision: int, material: CredentialMaterial
    ) -> Connection:
        """Fence earlier work and publish a private immutable credential version."""
        if not material.auth_method or not isinstance(material.credentials, dict):
            raise ValueError("invalid credential material")
        path = key.path(self.scope)
        try:
            record, head = await self._read_record(key)
        except ObjectNotFoundError:
            if expected_revision != 0:
                raise
            record = {"revision": 0, "fence": 0}
            head = None
        if record["revision"] != expected_revision:
            raise ObjectConflictError("connection revision conflict")
        fence = int(record["fence"]) + 1
        expiry = material.expires_at.isoformat().replace("+00:00", "Z") if material.expires_at else None
        envelope: dict[str, object] = {
            "schemaVersion": CONNECTION_SCHEMA_VERSION,
            "scope": self.scope.to_wire(),
            "connection": key.to_wire(),
            "fence": fence,
            "credentials": dict(sorted(material.credentials.items())),
        }
        if expiry is not None:
            envelope["expiresAt"] = expiry
        envelope["authMethod"] = material.auth_method
        contents = json_bytes(envelope)
        immutable_key = f"{path}/credentials/{digest_bytes(contents)[7:]}"
        try:
            candidate = await self.objects.create_object(immutable_key, contents)
        except (ObjectConflictError, OutcomeUnknownError):
            candidate = await self.objects.read_object(immutable_key)
            if candidate.contents != contents:
                raise ValueError("immutable credential content differs") from None
        reference = {"key": immutable_key, "version": candidate.version, "digest": digest_bytes(contents)[7:]}
        new_record: dict[str, object] = {
            "schemaVersion": CONNECTION_SCHEMA_VERSION,
            "connection": key.to_wire(),
            "scope": self.scope.to_wire(),
            "revision": expected_revision + 1,
            "fence": fence,
            "status": "READY",
        }
        if expiry is not None:
            new_record["expiresAt"] = expiry
        new_record["authMethod"] = material.auth_method
        new_record["credential"] = reference
        head_contents = json_bytes(new_record)
        try:
            if head is None:
                await self.objects.create_object(f"{path}/head", head_contents)
            else:
                await self.objects.compare_and_swap_object(f"{path}/head", head.etag, head_contents)
        except OutcomeUnknownError:
            observed = await self.objects.read_object(f"{path}/head")
            if observed.contents != head_contents:
                raise
        return self._connection(key, new_record)

    async def _read_record(self, key: ConnectionKey) -> tuple[dict[str, Any], Object]:
        path = key.path(self.scope)
        item = await self.objects.read_object(f"{path}/head")
        record = require_object(json.loads(item.contents), "connection head")
        if (
            not item.version
            or not item.etag
            or not set(record)
            <= {
                "schemaVersion",
                "connection",
                "scope",
                "revision",
                "fence",
                "status",
                "expiresAt",
                "authMethod",
                "credential",
                "mutation",
            }
            or record.get("schemaVersion") != CONNECTION_SCHEMA_VERSION
            or record.get("scope") != self.scope.to_wire()
            or record.get("connection") != key.to_wire()
            or not isinstance(record.get("revision"), int)
            or record["revision"] <= 0
            or not isinstance(record.get("fence"), int)
            or record["fence"] <= 0
            or record.get("status")
            not in {"READY", "REFRESHING", "AUTHORIZING", "REAUTHORIZATION_REQUIRED", "REVOKED"}
        ):
            raise ValueError("project connection identity is invalid")
        reference = record.get("credential")
        if record["status"] in ("READY", "REFRESHING") and reference is None:
            raise ValueError("project credential reference is absent")
        if record["status"] == "REVOKED" and (reference is not None or record.get("mutation") is not None):
            raise ValueError("revoked connection retains active material")
        if reference is not None:
            credential_ref = require_object(reference, "credential reference")
            if (
                not str(credential_ref.get("key", "")).startswith(path + "/")
                or not isinstance(credential_ref.get("version"), str)
                or not credential_ref["version"]
                or not isinstance(credential_ref.get("digest"), str)
                or not re.fullmatch(r"[0-9a-f]{64}", credential_ref["digest"])
            ):
                raise ValueError("project credential reference is invalid")
        return record, item

    def _connection(self, key: ConnectionKey, record: dict[str, Any]) -> Connection:
        return Connection(
            key,
            self.scope,
            record["revision"],
            record["fence"],
            str(record["status"]),
            self._parse_time(record.get("expiresAt")),
            str(record.get("authMethod", "")),
        )

    @staticmethod
    def _parse_time(value: object) -> datetime | None:
        if value is None:
            return None
        if not isinstance(value, str):
            raise ValueError("invalid credential expiry")
        return datetime.fromisoformat(value.replace("Z", "+00:00"))
