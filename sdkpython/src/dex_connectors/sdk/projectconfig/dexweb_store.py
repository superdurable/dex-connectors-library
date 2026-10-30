# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Process-local Dex Web credential projection and guarded file writeback."""

import asyncio
import json
import logging
import os
import tempfile
from datetime import UTC, datetime
from pathlib import Path

from ..timestamps import RFC3339NanoTimestamp, format_rfc3339_nano
from .connections import ConnectionKey, ConnectionStore, CredentialMaterial
from .models import Scope, require_object
from .storage import InMemoryObjectStore, Object, ObjectNotFoundError

_LOG = logging.getLogger(__name__)


def _credential_digest(material: CredentialMaterial) -> str:
    """Identify local material independently of JSON member order and spacing."""
    import hashlib

    canonical = json.dumps(
        material.credentials, separators=(",", ":"), sort_keys=True, ensure_ascii=False
    )
    for character, escaped in (
        ("<", "\\u003c"), (">", "\\u003e"), ("&", "\\u0026"),
        ("\u2028", "\\u2028"), ("\u2029", "\\u2029"),
    ):
        canonical = canonical.replace(character, escaped)
    contents = canonical.encode()
    if material.expires_at is not None:
        expiry = (
            material.expires_at.parsed
            if isinstance(material.expires_at, RFC3339NanoTimestamp)
            else material.expires_at
        )
        contents += b"|" + format_rfc3339_nano(expiry.astimezone(UTC)).encode()
    return hashlib.sha256(contents).hexdigest()


class DexWebObjectStore:
    """Project object store backed by memory and Dex Web's owner-only file.

    Connection head reads adopt changed credentials. Successful head writes
    publish refreshed material back to the file only if Dex Web has not saved
    another credential. This matches Go's process-local synchronization scope.
    """

    def __init__(self, connections_path: Path, scope: Scope) -> None:
        self._path = connections_path
        self._memory = InMemoryObjectStore()
        self._connections = ConnectionStore(self._memory, scope)
        self._lock = asyncio.Lock()
        self._imported: dict[ConnectionKey, str] = {}

    async def initialize(self) -> None:
        """Import current private credentials before application registration."""
        async with self._lock:
            await self._sync()

    async def read_object(self, key: str, version: str = "") -> Object:
        """Read a version, first adopting Dex Web updates for connection heads."""
        if _is_connection_head(key):
            async with self._lock:
                await self._sync()
                return await self._memory.read_object(key, version)
        return await self._memory.read_object(key, version)

    async def create_object(self, key: str, contents: bytes) -> Object:
        """Create one immutable or head object in process memory."""
        return await self._memory.create_object(key, contents)

    async def compare_and_swap_object(
        self, key: str, expected_etag: str, contents: bytes
    ) -> Object:
        """CAS a head and conditionally write refreshed credentials to Dex Web."""
        if not _is_connection_head(key):
            return await self._memory.compare_and_swap_object(key, expected_etag, contents)
        async with self._lock:
            await self._sync()
            result = await self._memory.compare_and_swap_object(key, expected_etag, contents)
            await self._write_back(result)
            return result

    async def _sync(self) -> None:
        from . import _decode_local_credentials, _read_private_file

        source = _read_private_file(self._path)
        for key, material in _decode_local_credentials(source).items():
            digest = _credential_digest(material)
            if self._imported.get(key) == digest:
                continue
            try:
                connection = await self._connections.read_connection(key)
                revision = connection.revision
            except ObjectNotFoundError:
                revision = 0
            await self._connections.replace_credential(key, revision, material)
            self._imported[key] = digest

    async def _write_back(self, head: Object) -> None:
        record = require_object(json.loads(head.contents), "connection head")
        if record.get("status") != "READY" or not isinstance(record.get("credential"), dict):
            return
        identity = require_object(record["connection"], "connection identity")
        key = ConnectionKey(str(identity["connectorId"]), str(identity["connectionName"]))
        try:
            material, _ = await self._connections.read_credential_material(key)
            digest = _credential_digest(material)
            if digest == self._imported.get(key):
                return
            self._replace_in_file(key, material)
            self._imported[key] = digest
        except (OSError, ValueError, KeyError, TypeError):
            _LOG.warning("local connector credential write-back skipped")

    def _replace_in_file(self, key: ConnectionKey, material: CredentialMaterial) -> None:
        from . import _read_private_file

        source = _read_private_file(self._path)
        records = source.get("connections")
        if not isinstance(records, list):
            raise ValueError("connections.json requires connections")
        for record in records:
            if not isinstance(record, dict):
                continue
            if (
                record.get("connectorId") != key.connector_id
                or record.get("connectionName") != key.connection_name
            ):
                continue
            prior = require_object(record.get("credentials"), "credentials")
            expiry = record.get("credentialExpiresAt")
            if expiry is not None and not isinstance(expiry, str):
                raise ValueError("local credential expiry is invalid")
            prior_expiry = datetime.fromisoformat(expiry.replace("Z", "+00:00")) if expiry else None
            prior_digest = _credential_digest(CredentialMaterial(prior, prior_expiry, material.auth_method))
            if prior_digest != self._imported.get(key):
                raise ValueError("the file holds a newer credential")
            record["credentials"] = material.credentials
            if material.expires_at is None:
                record.pop("credentialExpiresAt", None)
            else:
                observed = (
                    material.expires_at.parsed
                    if isinstance(material.expires_at, RFC3339NanoTimestamp)
                    else material.expires_at
                )
                record["credentialExpiresAt"] = format_rfc3339_nano(observed.astimezone(UTC))
            encoded = (json.dumps(source, indent=2, ensure_ascii=False) + "\n").encode()
            self._write_private_file(encoded)
            return
        raise ValueError("the connection is no longer in the file")

    def _write_private_file(self, contents: bytes) -> None:
        descriptor, temporary_name = tempfile.mkstemp(prefix=".write-", dir=self._path.parent)
        try:
            os.fchmod(descriptor, 0o600)
            with os.fdopen(descriptor, "wb") as temporary:
                temporary.write(contents)
                temporary.flush()
                os.fsync(temporary.fileno())
            os.replace(temporary_name, self._path)
        finally:
            Path(temporary_name).unlink(missing_ok=True)


def _is_connection_head(key: str) -> bool:
    return "/connections/" in key and key.endswith("/head")


__all__ = ["DexWebObjectStore"]
