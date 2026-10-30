# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Project configuration and credential protocols shared with Dex Web and Go."""

import ipaddress
import json
import os
import stat
from collections.abc import Mapping
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

import httpx

from .broker import CredentialBrokerClient, SharedConnection
from .connections import ConnectionKey, ConnectionStore, CredentialMaterial
from .dexweb_store import DexWebObjectStore
from .models import (
    CONFIGURATION_SCHEMA_VERSION,
    LOCAL_CONNECTIONS_SCHEMA_VERSION,
    LOCAL_USE_CONFIGURATIONS_SCHEMA_VERSION,
    ApplicationSecretRef,
    Configuration,
    ConnectionConfiguration,
    EnvironmentValue,
    Scope,
    SnapshotRef,
    require_object,
)
from .storage import ConfigurationStore, InMemoryObjectStore, ObjectStore, S3ObjectStore

LOCAL_CONNECTIONS_FILE_ENVIRONMENT = "DEX_CONNECTOR_CONFIG_FILE"


@dataclass(slots=True)
class LoadedProject:
    """Pinned ordinary snapshot with independently resolved current credentials."""

    configuration: Configuration
    snapshot: SnapshotRef
    connections: ConnectionStore
    configurations: ConfigurationStore
    broker: CredentialBrokerClient | None = None
    local_credentials: dict[ConnectionKey, CredentialMaterial] | None = None
    local_connections_path: Path | None = None
    shared_connections: dict[ConnectionKey, SharedConnection] | None = None

    async def resolve_credential(self, key: ConnectionKey, call_id: str = "") -> dict[str, Any]:
        """Resolve private material at call time without copying it into a Flow."""
        if self.shared_connections and key in self.shared_connections:
            return await self.shared_connections[key].resolve(call_id)
        if self.local_credentials is not None:
            if self.local_connections_path is not None:
                self.local_credentials = _decode_local_credentials(
                    _read_private_file(self.local_connections_path)
                )
            material = self.local_credentials.get(key)
            if material is None:
                raise LookupError("project connection not found")
            return material.credentials
        material, _ = await self.connections.read_credential_material(key)
        return material.credentials

    async def resolve_after_rejection(self, key: ConnectionKey, call_id: str) -> dict[str, Any]:
        """Ask the broker for a newer revision after a shared credential rejection."""
        if not self.shared_connections or key not in self.shared_connections:
            raise ValueError("connection does not use a shared credential broker")
        return await self.shared_connections[key].resolve_after_rejection(call_id)

    async def resolve_application_environment(self) -> dict[str, str]:
        """Load only pinned secret versions from the accepted configuration."""
        result: dict[str, str] = {}
        for name, value in self.configuration.environment.items():
            if value.value is not None:
                result[name] = value.value
            elif value.secret_ref is not None:
                result[name] = await self._read_secret(value.secret_ref, name)
        return result

    async def _read_secret(self, reference: ApplicationSecretRef, name: str) -> str:
        return await self.configurations.read_secret_value(reference, name)

    async def close(self) -> None:
        """Close an internally owned credential broker client."""
        if self.broker is not None:
            await self.broker.close()


async def load_project_from_object_store(
    objects: ObjectStore, scope: Scope, reference: SnapshotRef,
    broker_client: httpx.AsyncClient | None = None,
    connection_store: ConnectionStore | None = None,
) -> LoadedProject:
    """Read a pinned snapshot from a trusted object store without fetching credentials."""
    configurations = ConfigurationStore(objects, scope)
    configuration = await configurations.read_snapshot(reference)
    connections = connection_store if connection_store is not None else ConnectionStore(objects, scope)
    project = LoadedProject(configuration, reference, connections, configurations)
    if configuration.credential_broker is not None:

        async def read_token(token_reference: ApplicationSecretRef) -> str:
            return await project._read_secret(token_reference, "DEX_CREDENTIAL_BROKER_TOKEN")

        async def current_reference() -> ApplicationSecretRef:
            current, _ = await configurations.read_configuration()
            if current.credential_broker is None:
                raise ValueError("credential broker token reference is absent")
            return current.credential_broker.token_secret_ref

        project.broker = CredentialBrokerClient(
            configuration.credential_broker, read_token, current_reference, broker_client
        )
        project.shared_connections = {
            ConnectionKey(entry.connector_id, entry.connection_name): SharedConnection(
                entry.shared_connection_id, project.broker
            )
            for entry in configuration.connections
            if entry.shared_connection_id
        }
    return project


def _read_private_file(path: Path) -> dict[str, Any]:
    """Read a Dex Web owner-only regular JSON file without following symlinks."""
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600:
        raise ValueError(f"{path.name} must be a regular 0600 file")
    return require_object(json.loads(path.read_bytes()), path.name)


def _is_local_storage_endpoint(value: str) -> bool:
    """Allow only an explicitly isolated local or cluster fixture endpoint."""
    parsed = urlsplit(value)
    if (
        parsed.scheme not in ("http", "https")
        or not parsed.hostname
        or parsed.username
        or parsed.password
        or parsed.query
        or parsed.fragment
        or parsed.path not in ("", "/")
    ):
        return False
    host = parsed.hostname
    if host in ("localhost", "host.docker.internal") or host.endswith(".svc.cluster.local"):
        return True
    try:
        address = ipaddress.ip_address(host)
    except ValueError:
        return False
    return address.is_loopback or address.is_private


def _decode_local_credentials(source: dict[str, Any]) -> dict[ConnectionKey, CredentialMaterial]:
    """Read current Dex Web credential objects without changing frozen settings."""
    if source.get("schemaVersion") != LOCAL_CONNECTIONS_SCHEMA_VERSION:
        raise ValueError("unsupported connector configuration schema version")
    raw_connections = source.get("connections")
    if not isinstance(raw_connections, list):
        raise ValueError("connections.json requires connections")
    credentials: dict[ConnectionKey, CredentialMaterial] = {}
    for index, raw in enumerate(raw_connections):
        record = require_object(raw, f"connection {index}")
        key = ConnectionKey(str(record.get("connectorId", "")), str(record.get("connectionName", "")))
        key.path(Scope("local", "preview"))
        if key in credentials:
            raise ValueError("duplicate local connection")
        material = require_object(record.get("credentials"), "credentials")
        methods = record.get("authMethodIds", [])
        auth_method = str(
            record.get("authMethodId")
            or (methods[0] if isinstance(methods, list) and len(methods) == 1 else "local")
        )
        expiry = record.get("credentialExpiresAt")
        if expiry is not None and not isinstance(expiry, str):
            raise ValueError("local credential expiry is invalid")
        credentials[key] = CredentialMaterial(
            material, datetime.fromisoformat(expiry.replace("Z", "+00:00")) if expiry else None, auth_method
        )
    return credentials


async def load_dex_web_project(path: str | Path) -> LoadedProject:
    """Load Dex Web's connections.json and use-configurations.json sidecar."""
    connections_path = Path(path).absolute()
    source = _read_private_file(connections_path)
    if source.get("schemaVersion") != LOCAL_CONNECTIONS_SCHEMA_VERSION:
        raise ValueError("unsupported connector configuration schema version")
    uses_path = connections_path.with_name("use-configurations.json")
    uses = _read_private_file(uses_path) if uses_path.exists() else {}
    if uses and uses.get("schemaVersion") != LOCAL_USE_CONFIGURATIONS_SCHEMA_VERSION:
        raise ValueError("unsupported connector use configuration schema version")
    raw_connections = source.get("connections")
    if not isinstance(raw_connections, list):
        raise ValueError("connections.json requires connections")
    entries: list[ConnectionConfiguration] = []
    for index, raw in enumerate(raw_connections):
        record = require_object(raw, f"connection {index}")
        entry = ConnectionConfiguration.from_wire(
            {
                key: value
                for key, value in record.items()
                if key
                in {
                    "connectorId",
                    "connectionName",
                    "modulePath",
                    "provider",
                    "configuration",
                    "authMethodId",
                    "sharedConnectionId",
                }
            }
        )
        if not all((entry.connector_id, entry.connection_name, entry.module_path, entry.provider)):
            raise ValueError("local connection identity is incomplete")
        entries.append(entry)
    scope = Scope("local", "preview")
    store = InMemoryObjectStore()
    configurations = ConfigurationStore(store, scope)
    document = Configuration(
        scope,
        1,
        tuple(entries),
        tuple(require_object(item, "trigger binding") for item in source.get("triggerBindings", [])),
        tuple(
            require_object(item, "operation configuration")
            for item in uses.get("operationConfigurations", [])
        ),
    )
    written, _ = await configurations.write_configuration(0, document)
    reference = await configurations.freeze_configuration(written.revision)
    local_objects = DexWebObjectStore(connections_path, scope)
    await local_objects.initialize()
    return await load_project_from_object_store(
        store, scope, reference, connection_store=ConnectionStore(local_objects, scope)
    )


async def load_from_environment(
    environment: Mapping[str, str] | None = None, s3_client: object | None = None
) -> LoadedProject:
    """Load either Dex Web local files or the trusted DEX_PROJECT_* S3 contract."""
    values = environment if environment is not None else os.environ
    local_path = values.get(LOCAL_CONNECTIONS_FILE_ENVIRONMENT, "")
    if local_path:
        if any(
            values.get(name)
            for name in (
                "DEX_PROJECT_ID",
                "DEX_PROJECT_SCOPE",
                "DEX_PROJECT_CONFIG_KEY",
                "DEX_PROJECT_STORAGE_BUCKET",
            )
        ):
            raise ValueError("local and hosted project configuration selectors conflict")
        return await load_dex_web_project(local_path)
    scope = Scope(values.get("DEX_PROJECT_ID", ""), values.get("DEX_PROJECT_SCOPE", ""))
    _ = scope.prefix
    allow_local = values.get("DEX_PROJECT_ALLOW_LOCAL_STORAGE", "false")
    if allow_local not in ("true", "false"):
        raise ValueError("project local storage opt-in must be true or false")
    endpoint = values.get("DEX_PROJECT_STORAGE_ENDPOINT", "")
    if endpoint and (allow_local != "true" or not _is_local_storage_endpoint(endpoint)):
        raise ValueError("custom project storage endpoint requires explicit local opt-in")
    if allow_local == "true" and not endpoint:
        raise ValueError("local project storage requires an endpoint")
    bucket = values.get("DEX_PROJECT_STORAGE_BUCKET", "")
    kms_key = values.get("DEX_PROJECT_STORAGE_KMS_KEY_ARN", "")
    if not bucket:
        raise ValueError("project storage bucket is required")
    if s3_client is None:
        import boto3
        from botocore.config import Config

        s3_client = boto3.client(
            "s3",
            endpoint_url=endpoint or None,
            config=Config(
                retries={"max_attempts": 0},
                s3={"addressing_style": "path"} if endpoint else None,
            ),
        )
    objects = await S3ObjectStore.create(
        s3_client, bucket, values.get("DEX_PROJECT_STORAGE_PREFIX", ""), kms_key, allow_local == "true"
    )
    reference = SnapshotRef(
        values.get("DEX_PROJECT_CONFIG_KEY", ""),
        values.get("DEX_PROJECT_CONFIG_VERSION", ""),
        values.get("DEX_PROJECT_CONFIG_DIGEST", ""),
    )
    return await load_project_from_object_store(objects, scope, reference)


__all__ = [
    "ApplicationSecretRef",
    "CONFIGURATION_SCHEMA_VERSION",
    "Configuration",
    "ConfigurationStore",
    "ConnectionConfiguration",
    "ConnectionKey",
    "ConnectionStore",
    "CredentialMaterial",
    "EnvironmentValue",
    "InMemoryObjectStore",
    "LoadedProject",
    "LOCAL_CONNECTIONS_FILE_ENVIRONMENT",
    "ObjectStore",
    "S3ObjectStore",
    "Scope",
    "SnapshotRef",
    "load_dex_web_project",
    "load_from_environment",
    "load_project_from_object_store",
]
