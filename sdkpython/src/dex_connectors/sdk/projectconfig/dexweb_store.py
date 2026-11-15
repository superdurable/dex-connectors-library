# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Process-local Dex Web credential projection and guarded file writeback."""

import asyncio
import base64
import hashlib
import json
import logging
import os
import re
import tempfile
from datetime import UTC, datetime
from pathlib import Path

from ..timestamps import RFC3339NanoTimestamp, format_rfc3339_nano
from .connections import ConnectionKey, ConnectionStore, CredentialMaterial
from .models import Scope, go_canonical_json_bytes, require_object
from .storage import (
    InMemoryObjectStore,
    Object,
    ObjectConflictError,
    ObjectNotFoundError,
    _local_object_byte_limit,
    validate_object_key,
)

_LOG = logging.getLogger(__name__)
_LOCAL_VERSION = re.compile(r"[1-9][0-9]*", re.ASCII)


def _credential_digest(material: CredentialMaterial) -> str:
    """Identify local material independently of JSON member order and spacing."""
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
        self._inboxes = _LocalFileObjectStore(connections_path.parent / "trigger-inboxes")
        self._memory = InMemoryObjectStore()
        self._connections = ConnectionStore(self._memory, scope)
        self._lock = asyncio.Lock()
        self._imported: dict[ConnectionKey, str] = {}

    async def initialize(self) -> None:
        """Load local Trigger inboxes, then import current private credentials before registration."""
        await self._inboxes.initialize()
        async with self._lock:
            await self._sync()

    async def read_object(self, key: str, version: str = "") -> Object:
        """Read a version, first adopting Dex Web updates for connection heads."""
        if _is_trigger_inbox_key(key):
            return await self._inboxes.read_object(key, version)
        if _is_connection_head(key):
            async with self._lock:
                await self._sync()
                return await self._memory.read_object(key, version)
        return await self._memory.read_object(key, version)

    async def create_object(self, key: str, contents: bytes) -> Object:
        """Create one object in process memory, or a Trigger inbox in its private file."""
        if _is_trigger_inbox_key(key):
            return await self._inboxes.create_object(key, contents)
        return await self._memory.create_object(key, contents)

    async def compare_and_swap_object(
        self, key: str, expected_etag: str, contents: bytes
    ) -> Object:
        """CAS a head and conditionally write refreshed credentials to Dex Web."""
        if _is_trigger_inbox_key(key):
            return await self._inboxes.compare_and_swap_object(key, expected_etag, contents)
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


class _LocalFileObjectStore:
    """Versioned objects kept in memory and as private files, in the Go and TypeScript local layout.

    Each object is `<sha256 of key>-<version>.json` holding its key, version, ETag, and base64 contents.
    """

    def __init__(self, directory: Path) -> None:
        self._directory = directory
        self._objects: dict[str, Object] = {}
        self._lock = asyncio.Lock()

    async def initialize(self) -> None:
        async with self._lock:
            loaded = await asyncio.to_thread(self._load_files)
            for key, versions in loaded.items():
                versions.sort(key=lambda item: int(item.version))
                # A crash between writing a new version and deleting the old one leaves both.
                for stale in versions[:-1]:
                    await asyncio.to_thread(self._remove_file, stale)
                self._objects[key] = versions[-1]

    def _load_files(self) -> dict[str, list[Object]]:
        self._directory.mkdir(mode=0o700, parents=True, exist_ok=True)
        loaded: dict[str, list[Object]] = {}
        for path in sorted(self._directory.iterdir()):
            if path.suffix != ".json" or path.name.startswith("."):
                continue
            loaded.setdefault((item := _decode_local_object(path.read_bytes())).key, []).append(item)
        return loaded

    async def read_object(self, key: str, version: str = "") -> Object:
        item = self._objects.get(key)
        if item is None or (version and item.version != version):
            raise ObjectNotFoundError("project object not found")
        return item

    async def create_object(self, key: str, contents: bytes) -> Object:
        async with self._lock:
            if key in self._objects:
                raise ObjectConflictError("project object already exists")
            return await self._write(key, contents, None)

    async def compare_and_swap_object(self, key: str, expected_etag: str, contents: bytes) -> Object:
        async with self._lock:
            current = self._objects.get(key)
            if current is None:
                raise ObjectNotFoundError("project object not found")
            if not expected_etag or current.etag != expected_etag:
                raise ObjectConflictError("project object compare-and-swap conflict")
            return await self._write(key, contents, current)

    async def _write(self, key: str, contents: bytes, previous: Object | None) -> Object:
        # The new version is durable before the superseded file is deleted, so a crash never loses the key.
        validate_object_key(key)
        if not contents or len(contents) > _local_object_byte_limit(key):
            raise ValueError("project object body is empty or oversized")
        version = str(int(previous.version) + 1 if previous is not None else 1)
        contents = bytes(contents)
        item = Object(key, version, f"sha256:{hashlib.sha256(contents).hexdigest()}-{version}", contents)
        encoded = go_canonical_json_bytes({
            "key": key, "version": version, "etag": item.etag,
            "contents": base64.b64encode(contents).decode("ascii"),
        })
        await asyncio.to_thread(self._write_file, self._file(item), encoded)
        self._objects[key] = item
        if previous is not None:
            await asyncio.to_thread(self._remove_file, previous)
        return item

    def _file(self, item: Object) -> Path:
        return self._directory / f"{hashlib.sha256(item.key.encode()).hexdigest()}-{item.version}.json"

    def _write_file(self, path: Path, contents: bytes) -> None:
        descriptor, temporary_name = tempfile.mkstemp(prefix=".write-", dir=self._directory)
        try:
            os.fchmod(descriptor, 0o600)
            with os.fdopen(descriptor, "wb") as temporary:
                temporary.write(contents)
                temporary.flush()
                os.fsync(temporary.fileno())
            os.replace(temporary_name, path)
        finally:
            Path(temporary_name).unlink(missing_ok=True)
        if os.name != "nt":
            directory = os.open(self._directory, os.O_RDONLY)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)

    def _remove_file(self, item: Object) -> None:
        # A failure leaves a stale file that the next load discards.
        try:
            self._file(item).unlink()
        except OSError:
            return


def _decode_local_object(contents: bytes) -> Object:
    try:
        record = json.loads(contents)
        if not isinstance(record, dict):
            raise ValueError("not an object")
        key, version, etag, encoded = (record.get(name) for name in ("key", "version", "etag", "contents"))
        if (
            not isinstance(key, str) or not key
            or not isinstance(etag, str) or not etag
            or not isinstance(version, str) or not _LOCAL_VERSION.fullmatch(version)
            or not isinstance(encoded, str)
        ):
            raise ValueError("invalid fields")
        return Object(key, version, etag, base64.b64decode(encoded, validate=True))
    except (ValueError, UnicodeDecodeError):
        raise ValueError("local project storage file is invalid") from None


def _is_trigger_inbox_key(key: str) -> bool:
    return "/trigger-inboxes/" in key


def _is_connection_head(key: str) -> bool:
    return "/connections/" in key and key.endswith("/head")


__all__ = ["DexWebObjectStore"]
