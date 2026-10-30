# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Versioned private project objects, snapshot CAS, and S3 boundary."""

import asyncio
import json
import re
import secrets
from dataclasses import dataclass
from typing import Any, Protocol

from .models import (
    ApplicationSecretRef,
    Configuration,
    Scope,
    SnapshotRef,
    digest_bytes,
    is_application_environment_name,
    json_bytes,
    require_object,
    validate_environment_value,
    validate_secret_reference,
)

MAXIMUM_OBJECT_BYTES = 4 << 20
_KMS_KEY_ARN = re.compile(r"^arn:[a-z0-9-]+:kms:[a-z0-9-]+:[0-9]{12}:key/[a-zA-Z0-9-]+$")


class ObjectNotFoundError(LookupError):
    """Requested project object or exact version does not exist."""


class ObjectConflictError(ValueError):
    """Conditional create or compare-and-swap lost to another writer."""


class OutcomeUnknownError(OSError):
    """A conditional write may have reached storage; reconcile before retry."""


@dataclass(frozen=True, slots=True)
class Object:
    """Versioned private bytes; never serialize or print contents."""

    key: str
    version: str
    etag: str
    contents: bytes

    def __repr__(self) -> str:
        """Show only object identity."""
        return f"Object(key={self.key!r}, version={self.version!r})"

    def __reduce__(self) -> tuple[Any, ...]:
        """Prevent private project object serialization."""
        raise TypeError("private project objects cannot be serialized")


class ObjectStore(Protocol):
    """Exact-version reads and single-attempt conditional writes."""

    async def read_object(self, key: str, version: str = "") -> Object:
        """Read the exact version or current head."""
        ...

    async def create_object(self, key: str, contents: bytes) -> Object:
        """Create only if the key is absent."""
        ...

    async def compare_and_swap_object(self, key: str, expected_etag: str, contents: bytes) -> Object:
        """Replace only when the current ETag matches."""
        ...


def validate_object_key(key: str) -> None:
    """Reject traversal, separators, controls, and oversized object identities."""
    if (
        not key
        or len(key) > 1024
        or key.startswith("/")
        or any(character in key for character in "\\\x00\r\n")
        or any(segment in ("", ".", "..") for segment in key.split("/"))
    ):
        raise ValueError("invalid project object key")


class InMemoryObjectStore:
    """Deterministic versioned store for local Dex Web files and unit tests."""

    def __init__(self) -> None:
        self._objects: dict[str, list[Object]] = {}
        self._lock = asyncio.Lock()

    async def read_object(self, key: str, version: str = "") -> Object:
        """Return one immutable version without exposing mutable store state."""
        validate_object_key(key)
        async with self._lock:
            versions = self._objects.get(key, [])
            for item in reversed(versions):
                if not version or item.version == version:
                    return item
        raise ObjectNotFoundError("project object not found")

    async def create_object(self, key: str, contents: bytes) -> Object:
        """Create a new key under an atomic lock."""
        validate_object_key(key)
        self._validate_contents(contents)
        async with self._lock:
            if key in self._objects:
                raise ObjectConflictError("project object already exists")
            return self._append(key, contents)

    async def compare_and_swap_object(self, key: str, expected_etag: str, contents: bytes) -> Object:
        """Append a version only when the current ETag equals the expected token."""
        validate_object_key(key)
        self._validate_contents(contents)
        async with self._lock:
            versions = self._objects.get(key, [])
            if not versions or not expected_etag or versions[-1].etag != expected_etag:
                raise ObjectConflictError("project object compare-and-swap conflict")
            return self._append(key, contents)

    def _append(self, key: str, contents: bytes) -> Object:
        version = str(len(self._objects.get(key, [])) + 1)
        item = Object(key, version, digest_bytes(contents), contents)
        self._objects.setdefault(key, []).append(item)
        return item

    @staticmethod
    def _validate_contents(contents: bytes) -> None:
        if not contents or len(contents) > MAXIMUM_OBJECT_BYTES:
            raise ValueError("project object body is empty or oversized")


class S3ObjectStore:
    """Versioned S3 object store with exact KMS and conditional-write boundaries."""

    def __init__(
        self,
        client: Any,
        bucket: str,
        prefix: str = "",
        kms_key_arn: str = "",
        allow_unencrypted: bool = False,
    ) -> None:
        if (
            client is None
            or not bucket
            or any(character in bucket for character in "/\\\x00")
            or not (_KMS_KEY_ARN.fullmatch(kms_key_arn) or (allow_unencrypted and not kms_key_arn))
        ):
            raise ValueError("versioned project storage and encryption configuration are required")
        if prefix:
            validate_object_key(prefix.rstrip("/"))
        self._client = client
        self._bucket = bucket
        self._prefix = prefix.rstrip("/") + "/" if prefix else ""
        self._kms_key_arn = kms_key_arn

    @classmethod
    async def create(
        cls,
        client: Any,
        bucket: str,
        prefix: str = "",
        kms_key_arn: str = "",
        allow_unencrypted: bool = False,
    ) -> "S3ObjectStore":
        """Validate bucket versioning before constructing a storage boundary."""
        store = cls(client, bucket, prefix, kms_key_arn, allow_unencrypted)
        response = await asyncio.to_thread(client.get_bucket_versioning, Bucket=bucket)
        if response.get("Status") != "Enabled":
            raise ValueError("project object bucket versioning must be enabled")
        return store

    async def read_object(self, key: str, version: str = "") -> Object:
        """Read bounded bytes at an exact S3 version and verify encryption."""
        validate_object_key(key)
        arguments = {"Bucket": self._bucket, "Key": self._prefix + key}
        if version:
            arguments["VersionId"] = version
        try:
            response = await asyncio.to_thread(self._client.get_object, **arguments)
        except Exception as error:
            self._classify_storage_error(error, False)
            raise AssertionError("unreachable") from error
        body = response.get("Body")
        if body is None:
            raise ValueError("project object body is unavailable")
        try:
            observed_version = response.get("VersionId", "")
            etag = response.get("ETag", "")
            if (
                not observed_version
                or observed_version == "null"
                or not etag
                or (version and observed_version != version)
            ):
                raise ValueError("project object lacks exact version identity")
            if self._kms_key_arn and (
                response.get("ServerSideEncryption") != "aws:kms"
                or response.get("SSEKMSKeyId") != self._kms_key_arn
            ):
                raise ValueError("project object encryption boundary differs")
            contents = await asyncio.to_thread(body.read, MAXIMUM_OBJECT_BYTES + 1)
        finally:
            await asyncio.to_thread(body.close)
        if not contents or len(contents) > MAXIMUM_OBJECT_BYTES:
            raise ValueError("project object body is unavailable or oversized")
        return Object(key, observed_version, etag, contents)

    async def create_object(self, key: str, contents: bytes) -> Object:
        """Write once using If-None-Match; reconcile ambiguous errors externally."""
        return await self._write_object(key, contents, "")

    async def compare_and_swap_object(self, key: str, expected_etag: str, contents: bytes) -> Object:
        """Write once using If-Match with the exact prior ETag."""
        if not expected_etag:
            raise ValueError("object compare-and-swap requires an ETag")
        return await self._write_object(key, contents, expected_etag)

    async def _write_object(self, key: str, contents: bytes, etag: str) -> Object:
        validate_object_key(key)
        InMemoryObjectStore._validate_contents(contents)
        arguments: dict[str, object] = {
            "Bucket": self._bucket,
            "Key": self._prefix + key,
            "Body": contents,
            "ContentType": "application/json",
        }
        arguments["IfMatch" if etag else "IfNoneMatch"] = etag or "*"
        if self._kms_key_arn:
            arguments["ServerSideEncryption"] = "aws:kms"
            arguments["SSEKMSKeyId"] = self._kms_key_arn
        try:
            response = await asyncio.to_thread(self._client.put_object, **arguments)
        except Exception as error:
            self._classify_storage_error(error, True)
            raise AssertionError("unreachable") from error
        version = response.get("VersionId", "")
        response_etag = response.get("ETag", "")
        if not version or version == "null" or not response_etag:
            raise OutcomeUnknownError("project write has no exact version identity")
        return Object(key, version, response_etag, contents)

    @staticmethod
    def _classify_storage_error(error: Exception, is_write: bool) -> None:
        response = getattr(error, "response", None)
        status = (
            response.get("ResponseMetadata", {}).get("HTTPStatusCode") if isinstance(response, dict) else None
        )
        if status == 404 and not is_write:
            raise ObjectNotFoundError("project object not found") from None
        if status in (409, 412):
            raise ObjectConflictError("project object conditional write conflict") from None
        if is_write:
            raise OutcomeUnknownError("project write outcome is unknown") from None
        raise OSError("project object read failed") from None


class ConfigurationStore:
    """Revision CAS and exact-version ordinary snapshot reads."""

    def __init__(self, objects: ObjectStore, scope: Scope) -> None:
        if objects is None:
            raise ValueError("configuration object store is required")
        self.objects = objects
        self.scope = scope
        self.key = f"{scope.prefix}/configuration/head"

    async def read_configuration(self) -> tuple[Configuration, Object]:
        """Read and validate the current document identity."""
        item = await self.objects.read_object(self.key)
        return self._decode(item), item

    async def write_configuration(
        self, expected_revision: int, configuration: Configuration
    ) -> tuple[Configuration, Object]:
        """Conditionally replace ordinary settings using the expected revision."""
        try:
            previous, previous_object = await self.read_configuration()
        except ObjectNotFoundError:
            if expected_revision != 0:
                raise
            previous = None
            previous_object = None
        if (previous.revision if previous else 0) != expected_revision:
            raise ObjectConflictError("configuration revision conflict")
        schema_version = (
            "connectors.dex.dev/project-configuration/v1alpha2"
            if configuration.credential_broker is None
            and not any(item.shared_connection_id for item in configuration.connections)
            else "connectors.dex.dev/project-configuration/v1alpha3"
        )
        updated = Configuration(
            self.scope,
            expected_revision + 1,
            configuration.connections,
            configuration.trigger_bindings,
            configuration.operation_configurations,
            configuration.environment,
            configuration.credential_broker,
            schema_version,
        )
        contents = json_bytes(updated.to_wire())
        try:
            if previous_object is None:
                item = await self.objects.create_object(self.key, contents)
            else:
                item = await self.objects.compare_and_swap_object(self.key, previous_object.etag, contents)
        except OutcomeUnknownError:
            observed = await self.objects.read_object(self.key)
            if observed.contents != contents:
                raise
            item = observed
        return updated, item

    async def freeze_configuration(self, expected_revision: int) -> SnapshotRef:
        """Pin the current exact version when the revision matches."""
        configuration, item = await self.read_configuration()
        if configuration.revision != expected_revision:
            raise ObjectConflictError("configuration revision conflict")
        return SnapshotRef(item.key, item.version, digest_bytes(item.contents))

    async def read_snapshot(self, reference: SnapshotRef) -> Configuration:
        """Verify exact scope, version, and full document digest."""
        reference.validate(self.scope)
        item = await self.objects.read_object(reference.key, reference.version)
        if digest_bytes(item.contents) != reference.digest:
            raise ValueError("configuration snapshot digest differs")
        return self._decode(item)

    async def create_application_secret(self, name: str, value: str) -> ApplicationSecretRef:
        """Write one immutable private value beneath this scope and name."""
        if not is_application_environment_name(name):
            raise ValueError("invalid application secret name")
        validate_environment_value(value)
        return await self._create_secret(name, value)

    async def create_credential_broker_token(self, token: str) -> ApplicationSecretRef:
        """Write one immutable scope token outside application environment names."""
        if not token:
            raise ValueError("credential broker token is required")
        validate_environment_value(token)
        return await self._create_secret("DEX_CREDENTIAL_BROKER_TOKEN", token)

    async def read_secret_value(self, reference: ApplicationSecretRef, name: str) -> str:
        """Read exact private bytes and verify digest, scope, and field identity."""
        validate_secret_reference(reference, self.scope, name)
        item = await self.objects.read_object(reference.key, reference.version)
        if item.version != reference.version or digest_bytes(item.contents) != reference.digest:
            raise ValueError("application secret integrity differs")
        record = require_object(json.loads(item.contents), "application secret")
        if (
            record.get("scope") != self.scope.to_wire()
            or record.get("name") != name
            or not isinstance(record.get("value"), str)
        ):
            raise ValueError("application secret scope or identity differs")
        validate_environment_value(record["value"])
        return str(record["value"])

    async def _create_secret(self, name: str, value: str) -> ApplicationSecretRef:
        key = f"{self.scope.prefix}/app-secrets/{name}/{secrets.token_hex(16)}"
        contents = json_bytes({"scope": self.scope.to_wire(), "name": name, "value": value})
        try:
            item = await self.objects.create_object(key, contents)
        except (ObjectConflictError, OutcomeUnknownError):
            item = await self.objects.read_object(key)
            if item.contents != contents:
                raise ValueError("application secret content differs") from None
        return ApplicationSecretRef(key, item.version, digest_bytes(contents))

    def _decode(self, item: Object) -> Configuration:
        if item.key != self.key or not item.version or not item.etag:
            raise ValueError("configuration document identity is invalid")
        try:
            configuration = Configuration.from_wire(json.loads(item.contents))
        except (UnicodeDecodeError, json.JSONDecodeError) as error:
            raise ValueError("configuration document is invalid JSON") from error
        if configuration.scope != self.scope:
            raise ValueError("configuration document scope differs")
        return configuration
