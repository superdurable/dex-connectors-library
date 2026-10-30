# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Wire models for project configuration, local connections, and snapshots."""

import hashlib
import ipaddress
import json
import re
from dataclasses import dataclass, field
from typing import Any
from urllib.parse import urlsplit

CONFIGURATION_SCHEMA_VERSION = "connectors.dex.dev/project-configuration/v1alpha3"
PREVIOUS_CONFIGURATION_SCHEMA_VERSION = "connectors.dex.dev/project-configuration/v1alpha2"
LOCAL_CONNECTIONS_SCHEMA_VERSION = "connectors.dex.dev/local-connections/v1alpha1"
LOCAL_USE_CONFIGURATIONS_SCHEMA_VERSION = "connectors.dex.dev/local-use-configurations/v1alpha1"
_SCOPE_ID = re.compile(r"^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$")
_CONNECTOR_ID = re.compile(r"^[a-z][a-z0-9-]{1,62}$")
_DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
_ENVIRONMENT_NAME = re.compile(r"^[A-Z][A-Z0-9_]{0,127}$")
_SECRET_ID = re.compile(r"^[0-9a-f]{32}$")
_SHARED_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$")
_RESERVED_NAMES = {
    "PATH",
    "HOME",
    "PORT",
    "HOST",
    "NODE_OPTIONS",
    "SSL_CERT_FILE",
    "SSL_CERT_DIR",
    "PUBLIC_BASE_URL",
    "HTTP_PROXY",
    "HTTPS_PROXY",
    "ALL_PROXY",
    "NO_PROXY",
}
_RESERVED_PREFIXES = ("SUPERVERSE_", "DEX_", "AWS_", "LD_", "GO", "GIT_")


def is_application_environment_name(name: str) -> bool:
    """Reject names reserved for deployment, SDK, trust, and proxy settings."""
    return (
        bool(_ENVIRONMENT_NAME.fullmatch(name))
        and name not in _RESERVED_NAMES
        and not name.startswith(_RESERVED_PREFIXES)
    )


def validate_environment_value(value: str) -> None:
    """Limit UTF-8 startup values to 32768 bytes without NUL characters."""
    if "\x00" in value or len(value.encode("utf-8")) > 32768:
        raise ValueError("invalid application environment value")


def validate_secret_reference(reference: "ApplicationSecretRef", scope: "Scope", name: str) -> None:
    """Require an exact secret object beneath this scope and field name."""
    prefix = f"{scope.prefix}/app-secrets/{name}/"
    if (
        not reference.key.startswith(prefix)
        or not _SECRET_ID.fullmatch(reference.key[len(prefix) :])
        or not reference.version
        or reference.version == "null"
        or not _DIGEST.fullmatch(reference.digest)
    ):
        raise ValueError("application secret reference differs from scope")


def validate_broker_endpoint(value: str) -> None:
    """Require a bounded absolute endpoint; private hosts alone may use HTTP."""
    parsed = urlsplit(value)
    if (
        len(value) > 2048
        or parsed.scheme not in ("http", "https")
        or not parsed.hostname
        or parsed.username
        or parsed.password
        or parsed.query
        or parsed.fragment
    ):
        raise ValueError("credential broker endpoint must be an absolute HTTP or HTTPS URL")
    if parsed.scheme == "https":
        return
    host = parsed.hostname
    is_private = (
        host in ("localhost", "host.docker.internal")
        or "." not in host
        or host.endswith(".svc")
        or host.endswith(".svc.cluster.local")
    )
    try:
        address = ipaddress.ip_address(host)
        is_private = is_private or address.is_private or address.is_loopback or address.is_link_local
    except ValueError:
        pass
    if not is_private:
        raise ValueError("credential broker HTTP endpoint must use a private host")


def digest_bytes(contents: bytes) -> str:
    """Return the shared SHA-256 object digest."""
    return "sha256:" + hashlib.sha256(contents).hexdigest()


def json_bytes(value: object) -> bytes:
    """Encode Go-compatible compact JSON for versioned object digests."""
    serialized = json.dumps(value, separators=(",", ":"), ensure_ascii=False)
    for character, escape in (
        ("<", "\\u003c"),
        (">", "\\u003e"),
        ("&", "\\u0026"),
        ("\u2028", "\\u2028"),
        ("\u2029", "\\u2029"),
    ):
        serialized = serialized.replace(character, escape)
    return serialized.encode("utf-8")


def require_object(value: object, label: str) -> dict[str, Any]:
    """Require a JSON object, preserving unknown members where permitted."""
    if not isinstance(value, dict) or any(not isinstance(key, str) for key in value):
        raise ValueError(f"{label} must be a JSON object")
    return value


def require_fields(record: dict[str, Any], allowed: set[str], label: str) -> None:
    """Reject unknown fields in strict Go project configuration objects."""
    if not set(record) <= allowed:
        raise ValueError(f"{label} has unknown fields")


@dataclass(frozen=True, slots=True)
class Scope:
    """Trusted project and Live or Preview storage boundary."""

    project_id: str
    kind: str

    @property
    def prefix(self) -> str:
        """Return the canonical scoped prefix after validation."""
        if not _SCOPE_ID.fullmatch(self.project_id) or self.kind not in ("live", "preview"):
            raise ValueError("invalid project configuration scope")
        return f"projects/{self.project_id}/{self.kind}"

    def to_wire(self) -> dict[str, str]:
        """Encode the Go scope shape."""
        _ = self.prefix
        return {"projectId": self.project_id, "kind": self.kind}

    @classmethod
    def from_wire(cls, value: object) -> "Scope":
        """Decode and validate a scope object."""
        record = require_object(value, "scope")
        require_fields(record, {"projectId", "kind"}, "scope")
        result = cls(str(record.get("projectId", "")), str(record.get("kind", "")))
        _ = result.prefix
        return result


@dataclass(frozen=True, slots=True)
class SnapshotRef:
    """Exact version and digest of an immutable ordinary configuration document."""

    key: str
    version: str
    digest: str
    media_type: str = "application/json"

    def validate(self, scope: Scope) -> None:
        """Require this scope's head, an exact version, and a SHA-256 digest."""
        if (
            self.key != f"{scope.prefix}/configuration/head"
            or not self.version
            or self.version == "null"
            or not _DIGEST.fullmatch(self.digest)
            or self.media_type != "application/json"
        ):
            raise ValueError("invalid configuration snapshot reference")

    def to_wire(self) -> dict[str, str]:
        """Encode the Go snapshot reference shape."""
        return {"key": self.key, "version": self.version, "digest": self.digest, "mediaType": self.media_type}

    @classmethod
    def from_wire(cls, value: object) -> "SnapshotRef":
        """Decode a complete exact-version configuration reference."""
        record = require_object(value, "snapshot")
        require_fields(record, {"key", "version", "digest", "mediaType"}, "snapshot")
        return cls(
            str(record.get("key", "")),
            str(record.get("version", "")),
            str(record.get("digest", "")),
            str(record.get("mediaType", "")),
        )


@dataclass(frozen=True, slots=True)
class ApplicationSecretRef:
    """Exact private object version that contains one application secret."""

    key: str
    version: str
    digest: str

    def to_wire(self) -> dict[str, str]:
        """Encode a pinned application secret reference."""
        return {"key": self.key, "version": self.version, "digest": self.digest}

    @classmethod
    def from_wire(cls, value: object) -> "ApplicationSecretRef":
        """Decode a private reference without loading its material."""
        record = require_object(value, "application secret reference")
        require_fields(record, {"key", "version", "digest"}, "application secret reference")
        result = cls(
            str(record.get("key", "")), str(record.get("version", "")), str(record.get("digest", ""))
        )
        if not result.version or not _DIGEST.fullmatch(result.digest):
            raise ValueError("invalid application secret reference")
        return result


@dataclass(frozen=True, slots=True)
class EnvironmentValue:
    """Either one ordinary value or one private pinned secret reference."""

    value: str | None = None
    secret_ref: ApplicationSecretRef | None = None

    def to_wire(self) -> dict[str, object]:
        """Encode one mutually exclusive application environment selection."""
        if (self.value is None) == (self.secret_ref is None):
            raise ValueError("environment value requires exactly one value or secret reference")
        if self.value is not None:
            return {"value": self.value}
        assert self.secret_ref is not None
        return {"secretRef": self.secret_ref.to_wire()}

    @classmethod
    def from_wire(cls, value: object) -> "EnvironmentValue":
        """Decode one environment selection without exposing secret contents."""
        record = require_object(value, "environment value")
        if set(record) == {"value"} and isinstance(record["value"], str):
            return cls(value=record["value"])
        if set(record) == {"secretRef"}:
            return cls(secret_ref=ApplicationSecretRef.from_wire(record["secretRef"]))
        raise ValueError("invalid environment selection")


@dataclass(frozen=True, slots=True)
class ConnectionConfiguration:
    """Ordinary settings and logical credential identity for one named connector."""

    connector_id: str
    connection_name: str
    module_path: str
    provider: str
    configuration: dict[str, Any]
    auth_method_id: str = ""
    shared_connection_id: str = ""

    def to_wire(self) -> dict[str, object]:
        """Encode the shared connection entry with omitted optional members."""
        result: dict[str, object] = {
            "connectorId": self.connector_id,
            "connectionName": self.connection_name,
            "modulePath": self.module_path,
            "provider": self.provider,
        }
        if self.auth_method_id:
            result["authMethodId"] = self.auth_method_id
        if self.shared_connection_id:
            result["sharedConnectionId"] = self.shared_connection_id
        result["configuration"] = self.configuration
        return result

    @classmethod
    def from_wire(cls, value: object) -> "ConnectionConfiguration":
        """Decode an ordinary or shared connection entry."""
        record = require_object(value, "connection")
        require_fields(
            record,
            {
                "connectorId",
                "connectionName",
                "modulePath",
                "provider",
                "configuration",
                "authMethodId",
                "sharedConnectionId",
            },
            "connection",
        )
        return cls(
            str(record.get("connectorId", "")),
            str(record.get("connectionName", "")),
            str(record.get("modulePath", "")),
            str(record.get("provider", "")),
            require_object(record.get("configuration"), "connection configuration"),
            str(record.get("authMethodId", "")),
            str(record.get("sharedConnectionId", "")),
        )


@dataclass(frozen=True, slots=True)
class CredentialBrokerConfiguration:
    """Broker endpoint and exact scope-token reference for shared credentials."""

    endpoint: str
    token_secret_ref: ApplicationSecretRef

    def to_wire(self) -> dict[str, object]:
        """Encode the broker configuration protocol."""
        return {"endpoint": self.endpoint, "tokenSecretRef": self.token_secret_ref.to_wire()}

    @classmethod
    def from_wire(cls, value: object) -> "CredentialBrokerConfiguration":
        """Decode one broker configuration."""
        record = require_object(value, "credential broker")
        require_fields(record, {"endpoint", "tokenSecretRef"}, "credential broker")
        return cls(
            str(record.get("endpoint", "")), ApplicationSecretRef.from_wire(record.get("tokenSecretRef"))
        )


@dataclass(frozen=True, slots=True)
class Configuration:
    """Revisioned, secret-free project configuration snapshot."""

    scope: Scope
    revision: int
    connections: tuple[ConnectionConfiguration, ...]
    trigger_bindings: tuple[dict[str, Any], ...] = ()
    operation_configurations: tuple[dict[str, Any], ...] = ()
    environment: dict[str, EnvironmentValue] = field(default_factory=dict)
    credential_broker: CredentialBrokerConfiguration | None = None
    schema_version: str = CONFIGURATION_SCHEMA_VERSION

    def validate(self) -> None:
        """Reject duplicate identities, invalid schema, and missing broker selection."""
        _ = self.scope.prefix
        if self.schema_version not in (CONFIGURATION_SCHEMA_VERSION, PREVIOUS_CONFIGURATION_SCHEMA_VERSION):
            raise ValueError("unsupported configuration schema")
        if self.revision < 1:
            raise ValueError("configuration revision is required")
        if (
            len(self.connections) > 1000
            or len(self.trigger_bindings) > 10000
            or len(self.operation_configurations) > 10000
        ):
            raise ValueError("configuration document exceeds binding limits")
        identities = [(entry.connector_id, entry.connection_name) for entry in self.connections]
        if len(identities) != len(set(identities)):
            raise ValueError("duplicate project connection")
        if any(
            not _CONNECTOR_ID.fullmatch(entry.connector_id)
            or not all((entry.connection_name, entry.module_path, entry.provider))
            or not isinstance(entry.configuration, dict)
            for entry in self.connections
        ):
            raise ValueError("invalid project connection")
        trigger_identities: set[tuple[str, str, str, str]] = set()
        for binding in self.trigger_bindings:
            trigger_identity = (
                str(binding.get("connectorId", "")),
                str(binding.get("connectionName", "")),
                str(binding.get("triggerName", "")),
                str(binding.get("bindingName", "")),
            )
            if (
                trigger_identity[:2] not in identities
                or not trigger_identity[2]
                or not trigger_identity[3]
                or not isinstance(binding.get("configuration"), dict)
                or trigger_identity in trigger_identities
            ):
                raise ValueError("configuration trigger is invalid or duplicated")
            trigger_identities.add(trigger_identity)
        operation_identities: set[tuple[str, str, str, str, str]] = set()
        for operation in self.operation_configurations:
            operation_identity = (
                str(operation.get("connectorId", "")),
                str(operation.get("connectionName", "")),
                str(operation.get("operationId", "")),
                str(operation.get("flowType", "")),
                str(operation.get("stepType", "")),
            )
            if (
                operation_identity[:2] not in identities
                or not all(operation_identity[2:])
                or not isinstance(operation.get("configuration"), dict)
                or operation_identity in operation_identities
            ):
                raise ValueError("configuration operation is invalid or duplicated")
            operation_identities.add(operation_identity)
        if any(entry.shared_connection_id for entry in self.connections) and self.credential_broker is None:
            raise ValueError("shared connection requires a credential broker")
        if (
            self.schema_version == PREVIOUS_CONFIGURATION_SCHEMA_VERSION
            and self.credential_broker is not None
        ):
            raise ValueError("previous configuration schema cannot declare a broker")
        if any(
            entry.shared_connection_id and not _SHARED_ID.fullmatch(entry.shared_connection_id)
            for entry in self.connections
        ):
            raise ValueError("configuration shared connection ID is invalid")
        if len(self.environment) > 128:
            raise ValueError("application environment exceeds field limit")
        for name, entry in self.environment.items():
            if not is_application_environment_name(name):
                raise ValueError("invalid application environment name")
            entry.to_wire()
            if entry.value is not None:
                validate_environment_value(entry.value)
            elif entry.secret_ref is not None:
                validate_secret_reference(entry.secret_ref, self.scope, name)
        if self.credential_broker is not None:
            validate_broker_endpoint(self.credential_broker.endpoint)
            validate_secret_reference(
                self.credential_broker.token_secret_ref, self.scope, "DEX_CREDENTIAL_BROKER_TOKEN"
            )

    def to_wire(self) -> dict[str, object]:
        """Encode the shared project configuration schema."""
        self.validate()
        result: dict[str, object] = {
            "schemaVersion": self.schema_version,
            "scope": self.scope.to_wire(),
            "revision": self.revision,
            "connections": [entry.to_wire() for entry in self.connections],
            "triggerBindings": list(self.trigger_bindings),
            "operationConfigurations": list(self.operation_configurations),
        }
        if self.environment:
            result["environment"] = {key: self.environment[key].to_wire() for key in sorted(self.environment)}
        if self.credential_broker is not None:
            result["credentialBroker"] = self.credential_broker.to_wire()
        return result

    @classmethod
    def from_wire(cls, value: object) -> "Configuration":
        """Decode and validate the shared document, including old schema documents."""
        record = require_object(value, "configuration")
        require_fields(
            record,
            {
                "schemaVersion",
                "scope",
                "revision",
                "connections",
                "triggerBindings",
                "operationConfigurations",
                "environment",
                "credentialBroker",
            },
            "configuration",
        )
        connections = record.get("connections")
        triggers = record.get("triggerBindings")
        operations = record.get("operationConfigurations")
        environment = require_object(record.get("environment", {}), "environment")
        if (
            not isinstance(connections, list)
            or not isinstance(triggers, list)
            or not isinstance(operations, list)
        ):
            raise ValueError("configuration arrays are required")
        broker = record.get("credentialBroker")
        result = cls(
            Scope.from_wire(record.get("scope")),
            int(record.get("revision", 0)),
            tuple(ConnectionConfiguration.from_wire(entry) for entry in connections),
            tuple(require_object(entry, "trigger") for entry in triggers),
            tuple(require_object(entry, "operation") for entry in operations),
            {key: EnvironmentValue.from_wire(item) for key, item in environment.items()},
            CredentialBrokerConfiguration.from_wire(broker) if broker is not None else None,
            str(record.get("schemaVersion", "")),
        )
        result.validate()
        return result
