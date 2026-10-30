# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Stable connector identities and wire-compatible result values."""

import hashlib
import math
import re
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from typing import Generic, Literal, TypeVar, cast
from uuid import NAMESPACE_URL, UUID, uuid5

from dex import AsyncContext

from .errors import Failure
from .retry_policy import RetryPolicy
from .timestamps import RFC3339NanoTimestamp, format_rfc3339_nano

T = TypeVar("T")
CONNECTOR_ID_PATTERN = re.compile(r"^[a-z][a-z0-9-]{1,62}$")
OPERATION_ID_PATTERN = re.compile(r"^[a-z][A-Za-z0-9]+$")
DEFECT_BRANCH_ID = "defect"
UNCERTAIN_BRANCH_ID = "uncertain"
_CALL_ID_NAMESPACE = uuid5(NAMESPACE_URL, "https://superdurable.dev/dex-connectors/call-id/v1")


class CallID(str):
    """Stable UUID identity of one provider call across Step retries."""

    def validate(self) -> None:
        """Require a UUID value."""
        try:
            UUID(self)
        except (ValueError, AttributeError) as error:
            raise ValueError("call ID must be a UUID") from error


def derive_call_id(flow_id: str, step_execution_id: str, operation: "OperationRef") -> CallID:
    """Derive Go's length-prefixed SHA-1 UUID for one Dex Step execution."""
    if not flow_id or not step_execution_id:
        raise ValueError("Dex Flow and Step execution IDs are required")
    operation.validate()
    fields = (flow_id, step_execution_id, operation.connector_id, operation.operation_id)
    payload = b"".join(len(field.encode()).to_bytes(4, "big") + field.encode() for field in fields)
    digest = hashlib.sha1(_CALL_ID_NAMESPACE.bytes + payload).digest()[:16]
    return CallID(str(UUID(bytes=digest, version=5)))


class IdempotencyKey(str):
    """Provider-safe representation of a stable call identity."""


@dataclass(frozen=True, slots=True)
class ConnectionRef:
    """Logical credential reference safe to persist in a Flow."""

    provider: str
    name: str

    def validate(self) -> None:
        """Require both logical connection fields."""
        if not self.provider.strip() or not self.name.strip():
            raise ValueError("connection provider and name are required")

    def to_wire(self) -> dict[str, str]:
        """Encode the Go-compatible connection reference."""
        self.validate()
        return {"provider": self.provider, "name": self.name}

    @classmethod
    def from_wire(cls, value: dict[str, str]) -> "ConnectionRef":
        """Decode a logical credential reference."""
        result = cls(value["provider"], value["name"])
        result.validate()
        return result


@dataclass(frozen=True, slots=True)
class OperationRef:
    """Stable manifest identity of a connector operation."""

    connector_id: str
    operation_id: str

    def validate(self) -> None:
        """Require a DNS-like connector ID and lower-camel operation ID."""
        if not CONNECTOR_ID_PATTERN.fullmatch(self.connector_id):
            raise ValueError("connector ID must be DNS-like and 2-63 characters")
        if not OPERATION_ID_PATTERN.fullmatch(self.operation_id):
            raise ValueError("operation ID must be lower camel case")

    def to_wire(self) -> dict[str, str]:
        """Encode the Go-compatible operation reference."""
        self.validate()
        return {"connectorId": self.connector_id, "operationId": self.operation_id}

    @classmethod
    def from_wire(cls, value: dict[str, str]) -> "OperationRef":
        """Decode a stable manifest operation identity."""
        result = cls(value["connectorId"], value["operationId"])
        result.validate()
        return result


@dataclass(frozen=True, slots=True)
class Call:
    """Provider-safe metadata and ephemeral async Dex Context for one invocation.

    The live context is available only during a Step handler and is never
    serialized, compared, or included in Flow state.
    """

    id: CallID
    connection: ConnectionRef
    operation: OperationRef
    idempotency_key: IdempotencyKey = IdempotencyKey("")
    context: AsyncContext | None = field(default=None, repr=False, compare=False)

    def to_wire(self) -> dict[str, object]:
        """Encode the Go Call's persisted identity fields."""
        self.id.validate()
        result: dict[str, object] = {
            "id": str(self.id),
            "connection": self.connection.to_wire(),
            "operation": self.operation.to_wire(),
        }
        if self.idempotency_key:
            result["idempotencyKey"] = str(self.idempotency_key)
        return result

    @classmethod
    def from_wire(cls, value: dict[str, object]) -> "Call":
        """Decode a Go call identity without constructing a Dex context."""
        result = cls(
            CallID(cast(str, value["id"])),
            ConnectionRef.from_wire(cast(dict[str, str], value["connection"])),
            OperationRef.from_wire(cast(dict[str, str], value["operation"])),
            IdempotencyKey(cast(str, value.get("idempotencyKey", ""))),
        )
        result.id.validate()
        return result


@dataclass(frozen=True, slots=True)
class BranchDefinition:
    """Stable operation route; only the happy path is required by default."""

    id: str
    description: str
    optional: bool = False

    def validate(self) -> None:
        """Require a unique lower-camel route and explanatory description."""
        if not OPERATION_ID_PATTERN.fullmatch(self.id) or not self.description.strip():
            raise ValueError("invalid branch identity or description")

    def to_wire(self) -> dict[str, object]:
        """Encode a branch with Go's omitted false optional field."""
        self.validate()
        result: dict[str, object] = {"id": self.id, "description": self.description}
        if self.optional:
            result["optional"] = True
        return result

    @classmethod
    def from_wire(cls, value: dict[str, object]) -> "BranchDefinition":
        """Decode one Go branch definition with its optional route flag."""
        result = cls(str(value["id"]), str(value["description"]), bool(value.get("optional", False)))
        result.validate()
        return result


@dataclass(frozen=True, slots=True)
class StepDefaults:
    """Execute-only Dex defaults owned by an operation definition."""

    execute_method_timeout: timedelta
    execute_durability: int
    heartbeat_timeout: timedelta = timedelta(0)
    execute_retry: RetryPolicy | None = None
    _wire_timeouts: tuple[int, int] | None = field(default=None, repr=False, compare=False)

    def validate(self) -> None:
        """Reject negative timeouts and unknown durability modes."""
        if self.execute_method_timeout < timedelta(0) or self.heartbeat_timeout < timedelta(0):
            raise ValueError("Step timeouts cannot be negative")
        if self.execute_durability not in (0, 1, 2):
            raise ValueError("Step Execute durability is invalid")
        if self.execute_retry is not None:
            self.execute_retry.validate()

    def to_wire(self) -> dict[str, object]:
        """Encode Go's nanosecond Step defaults."""
        self.validate()
        execute_timeout_ns = int(self.execute_method_timeout.total_seconds() * 1_000_000_000)
        heartbeat_timeout_ns = int(self.heartbeat_timeout.total_seconds() * 1_000_000_000)
        raw = self._wire_timeouts
        if raw is not None and (
            self.execute_method_timeout == timedelta(microseconds=raw[0] / 1000)
            and self.heartbeat_timeout == timedelta(microseconds=raw[1] / 1000)
        ):
            execute_timeout_ns, heartbeat_timeout_ns = raw
        result: dict[str, object] = {
            "executeMethodTimeout": execute_timeout_ns,
            "executeDurability": self.execute_durability,
        }
        if self.heartbeat_timeout or (raw is not None and raw[1] != 0):
            result["heartbeatTimeout"] = heartbeat_timeout_ns
        if self.execute_retry is not None:
            result["executeRetry"] = self.execute_retry.to_wire()
        return result

    @classmethod
    def from_wire(cls, value: dict[str, object]) -> "StepDefaults":
        """Decode operation Step defaults without registering a Dex Step."""
        retry = value.get("executeRetry")
        result = cls(
            timedelta(microseconds=cast(int, value["executeMethodTimeout"]) / 1000),
            cast(int, value["executeDurability"]),
            timedelta(microseconds=cast(int, value.get("heartbeatTimeout", 0)) / 1000),
            RetryPolicy.from_wire(cast(dict[str, int | float], retry)) if retry is not None else None,
            (cast(int, value["executeMethodTimeout"]), cast(int, value.get("heartbeatTimeout", 0))),
        )
        result.validate()
        return result


@dataclass(frozen=True, slots=True)
class QueryDefinition:
    """Query identity, stable routes, and Execute defaults."""

    operation: OperationRef
    branches: tuple[BranchDefinition, ...] | None
    step_defaults: StepDefaults

    def validate(self) -> None:
        """Require distinct routes, a defect branch, and no uncertainty route."""
        self.operation.validate()
        if self.branches is None or not self.branches:
            self.step_defaults.validate()
            return
        ids = [branch.id for branch in self.branches]
        if len(ids) != len(set(ids)) or DEFECT_BRANCH_ID not in ids or UNCERTAIN_BRANCH_ID in ids:
            raise ValueError("invalid Query branches")
        for branch in self.branches:
            branch.validate()
        self.step_defaults.validate()

    def to_wire(self) -> dict[str, object]:
        """Encode the Go Query definition shape."""
        self.validate()
        return {
            "operation": self.operation.to_wire(),
            "branches": [branch.to_wire() for branch in self.branches] if self.branches is not None else None,
            "stepDefaults": self.step_defaults.to_wire(),
        }

    @classmethod
    def from_wire(cls, value: dict[str, object]) -> "QueryDefinition":
        """Decode a Go Query definition."""
        operation = cast(dict[str, str], value["operation"])
        branches = cast(list[dict[str, object]] | None, value["branches"])
        result = cls(
            OperationRef(operation["connectorId"], operation["operationId"]),
            tuple(BranchDefinition.from_wire(branch) for branch in branches)
            if branches is not None
            else None,
            StepDefaults.from_wire(cast(dict[str, object], value["stepDefaults"])),
        )
        result.validate()
        return result


@dataclass(frozen=True, slots=True)
class MutationDefinition(QueryDefinition):
    """Mutation identity, stable routes, and Execute defaults including uncertainty."""

    def validate(self) -> None:
        """Require distinct routes and a defect branch; uncertainty is optional."""
        self.operation.validate()
        if self.branches is None or not self.branches:
            self.step_defaults.validate()
            return
        ids = [branch.id for branch in self.branches]
        if len(ids) != len(set(ids)) or DEFECT_BRANCH_ID not in ids:
            raise ValueError("invalid Mutation branches")
        for branch in self.branches:
            branch.validate()
        self.step_defaults.validate()


@dataclass(frozen=True, slots=True)
class Receipt:
    """Provider correlation data; callers must exclude secrets before construction."""

    call_id: CallID
    provider: str
    observed_at: RFC3339NanoTimestamp | datetime
    idempotency_key: IdempotencyKey = IdempotencyKey("")
    provider_object_id: str = ""
    provider_request_id: str = ""
    metadata: dict[str, str] = field(default_factory=dict)

    def to_wire(self) -> dict[str, object]:
        """Encode Go's camel-case receipt and RFC 3339 timestamp."""
        result: dict[str, object] = {
            "callId": str(self.call_id),
            "provider": self.provider,
            "observedAt": format_rfc3339_nano(self.observed_at),
        }
        if self.idempotency_key:
            result["idempotencyKey"] = str(self.idempotency_key)
        if self.provider_object_id:
            result["providerObjectId"] = self.provider_object_id
        if self.provider_request_id:
            result["providerRequestId"] = self.provider_request_id
        if self.metadata:
            result["metadata"] = self.metadata.copy()
        return result

    @classmethod
    def from_wire(cls, value: object) -> "Receipt":
        """Decode the shared receipt shape with optional Go fields."""
        if not isinstance(value, dict):
            raise ValueError("receipt must be an object")
        if (
            not isinstance(value.get("callId"), str)
            or not isinstance(value.get("provider"), str)
            or not isinstance(value.get("observedAt"), str)
        ):
            raise ValueError("receipt fields are invalid")
        metadata = value.get("metadata", {})
        if not isinstance(metadata, dict) or any(
            not isinstance(k, str) or not isinstance(v, str) for k, v in metadata.items()
        ):
            raise ValueError("receipt metadata is invalid")
        call_id = CallID(value["callId"])
        call_id.validate()
        return cls(
            call_id,
            value["provider"],
            RFC3339NanoTimestamp.from_wire(value["observedAt"]),
            IdempotencyKey(value.get("idempotencyKey", "")),
            value.get("providerObjectId", ""),
            value.get("providerRequestId", ""),
            metadata,
        )


@dataclass(frozen=True, slots=True)
class QueryResult(Generic[T]):
    """Validated terminal Query route, typed value, receipt, and optional failure."""

    branch: str
    value: T
    receipt: Receipt
    failure: Failure | None = None

    def __post_init__(self) -> None:
        """Reject malformed Query routes before they enter Flow state."""
        if not self.branch or self.branch == UNCERTAIN_BRANCH_ID or not isinstance(self.receipt, Receipt):
            raise ValueError("Query result branch is invalid")
        if self.failure is not None:
            self.failure.validate()

    def to_wire(self) -> dict[str, object]:
        """Encode a Query result with Go's optional failure omission."""
        result: dict[str, object] = {
            "branch": self.branch,
            "value": self.value,
            "receipt": self.receipt.to_wire(),
        }
        if self.failure is not None:
            result["failure"] = self.failure.to_wire()
        return result

    @classmethod
    def from_wire(cls, value: dict[str, object]) -> "QueryResult[T]":
        """Decode a result without erasing its operation-specific value."""
        failure = Failure.from_wire(value["failure"]) if "failure" in value else None
        return cls(
            str(value["branch"]), cast(T, value["value"]), Receipt.from_wire(value["receipt"]), failure
        )


@dataclass(frozen=True, slots=True)
class MutationResult(Generic[T]):
    """Validated terminal Mutation route with the same wire shape as QueryResult."""

    branch: str
    value: T
    receipt: Receipt
    failure: Failure | None = None

    def __post_init__(self) -> None:
        """Require failure context for an uncertain write route."""
        if (
            not self.branch
            or not isinstance(self.receipt, Receipt)
            or (self.branch == UNCERTAIN_BRANCH_ID and self.failure is None)
        ):
            raise ValueError("Mutation result branch is invalid")
        if self.failure is not None:
            self.failure.validate()

    def to_wire(self) -> dict[str, object]:
        """Encode a Mutation result with Go's optional failure omission."""
        result: dict[str, object] = {
            "branch": self.branch,
            "value": self.value,
            "receipt": self.receipt.to_wire(),
        }
        if self.failure is not None:
            result["failure"] = self.failure.to_wire()
        return result

    @classmethod
    def from_wire(cls, value: dict[str, object]) -> "MutationResult[T]":
        """Decode a Mutation result without erasing its typed value."""
        failure = Failure.from_wire(value["failure"]) if "failure" in value else None
        return cls(
            str(value["branch"]), cast(T, value["value"]), Receipt.from_wire(value["receipt"]), failure
        )


@dataclass(frozen=True, slots=True)
class QueryAttempt(Generic[T]):
    """One provider Query outcome before Step factory validation."""

    kind: Literal["branch", "retry"]
    branch: str = ""
    value: T | None = None
    receipt: Receipt | None = None
    failure: Failure | None = None
    retry_after_seconds: float = 0

    def __post_init__(self) -> None:
        """Keep retry and terminal outcomes disjoint."""
        if self.kind == "retry":
            if (
                self.failure is None
                or self.retry_after_seconds < 0
                or not math.isfinite(self.retry_after_seconds)
                or self.branch
                or self.receipt is not None
                or self.value is not None
            ):
                raise ValueError("Query retry requires failure and nonnegative delay")
        elif self.kind == "branch":
            if (
                not self.branch
                or self.branch == UNCERTAIN_BRANCH_ID
                or self.receipt is None
                or self.retry_after_seconds != 0
            ):
                raise ValueError("Query branch requires route and receipt")
        else:
            raise ValueError("Query attempt kind is invalid")
        if self.failure is not None:
            self.failure.validate()

    @classmethod
    def branch_result(
        cls, branch: str, value: T, receipt: Receipt, failure: Failure | None = None
    ) -> "QueryAttempt[T]":
        """Construct a definite business outcome."""
        return cls("branch", branch, value, receipt, failure)

    @classmethod
    def retry(cls, failure: Failure, retry_after_seconds: float = 0) -> "QueryAttempt[T]":
        """Construct a retryable Query failure."""
        return cls("retry", failure=failure, retry_after_seconds=retry_after_seconds)


@dataclass(frozen=True, slots=True)
class MutationAttempt(Generic[T]):
    """One provider Mutation outcome, including narrowly justified uncertainty."""

    kind: Literal["branch", "retry", "uncertain"]
    branch: str = ""
    value: T | None = None
    receipt: Receipt | None = None
    failure: Failure | None = None
    retry_after_seconds: float = 0

    def __post_init__(self) -> None:
        """Require evidence context for each Mutation outcome category."""
        if self.kind == "retry":
            if (
                self.failure is None
                or self.retry_after_seconds < 0
                or not math.isfinite(self.retry_after_seconds)
                or self.branch
                or self.receipt is not None
                or self.value is not None
            ):
                raise ValueError("Mutation retry requires failure and nonnegative delay")
        elif self.kind in ("branch", "uncertain"):
            if not self.branch or self.receipt is None or self.retry_after_seconds != 0:
                raise ValueError("Mutation terminal outcome requires route and receipt")
            if self.kind == "branch" and self.branch == UNCERTAIN_BRANCH_ID:
                raise ValueError("uncertainty requires an uncertain Mutation attempt")
            if self.kind == "uncertain" and (self.branch != UNCERTAIN_BRANCH_ID or self.failure is None):
                raise ValueError("uncertain Mutation requires failure and uncertainty route")
        else:
            raise ValueError("Mutation attempt kind is invalid")
        if self.failure is not None:
            self.failure.validate()

    @classmethod
    def branch_result(
        cls, branch: str, value: T, receipt: Receipt, failure: Failure | None = None
    ) -> "MutationAttempt[T]":
        """Construct a definite Mutation business outcome."""
        return cls("branch", branch, value, receipt, failure)

    @classmethod
    def retry(cls, failure: Failure, retry_after_seconds: float = 0) -> "MutationAttempt[T]":
        """Construct a retryable Mutation failure."""
        return cls("retry", failure=failure, retry_after_seconds=retry_after_seconds)

    @classmethod
    def uncertain(cls, value: T, failure: Failure, receipt: Receipt) -> "MutationAttempt[T]":
        """Record an unconfirmable possibly accepted, non-repeatable write."""
        return cls("uncertain", UNCERTAIN_BRANCH_ID, value, receipt, failure)
