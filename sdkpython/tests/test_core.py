# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Identity, result, failure, retry, configuration UI, and credential tests."""

import json
import pickle
from datetime import UTC, datetime, timedelta, timezone
from pathlib import Path
from typing import Any, cast

import pytest

from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    Failure,
    FailureKind,
    MutationAttempt,
    MutationDefinition,
    MutationResult,
    OperationRef,
    QueryAttempt,
    QueryDefinition,
    QueryResult,
    ReauthorizationRequiredError,
    Receipt,
    RetryError,
    RetryPolicy,
    SecretString,
    StepDefaults,
    default_connector_step_execute_retry_policy,
    derive_call_id,
)
from dex_connectors.sdk.credentials import CredentialRefreshResult, CredentialRefreshState
from dex_connectors.sdk.projectconfig.models import Scope, SnapshotRef
from dex_connectors.sdk.timestamps import RFC3339NanoTimestamp


def test_go_identity_and_results_round_trip(conformance_dir: Path) -> None:
    """Preserve UUID derivation, optional omissions, and receipt wire names."""
    identity = json.loads((conformance_dir / "identity.json").read_text())
    source = identity["source"]
    derived = derive_call_id(
        source["flowId"],
        source["stepExecutionId"],
        OperationRef(source["connectorId"], source["operationId"]),
    )
    assert derived == identity["derivedCall"]["id"]
    assert Call.from_wire(identity["derivedCall"]).to_wire() == identity["derivedCall"]
    assert derive_call_id(source["flowId"], "other", OperationRef("sample", "createItem")) != derived
    CallID(derived).validate()
    assert identity["callIDNamespace"] == "63281a42-70ad-5bc3-a289-728819af723f"
    assert (
        derive_call_id(source["flowId"], "step-execution-8", OperationRef("sample", "createItem"))
        == identity["differentStepCallId"]
    )
    assert (
        derive_call_id(source["flowId"], source["stepExecutionId"], OperationRef("sample", "getItem"))
        == identity["differentOperationCallId"]
    )
    assert identity["providerKey"] == f"sample/{derived}"
    assert (
        Receipt.from_wire(identity["fallbackCompletedReceipt"]).to_wire()
        == identity["fallbackCompletedReceipt"]
    )
    unicode_source = identity["unicodeSource"]
    assert (
        derive_call_id(
            unicode_source["flowId"],
            unicode_source["stepExecutionId"],
            OperationRef(unicode_source["connectorId"], unicode_source["operationId"]),
        )
        == identity["unicodeDerivedCallId"]
    )
    results = json.loads((conformance_dir / "results.json").read_text())
    for name, value in results.items():
        if name.endswith("Receipt"):
            assert Receipt.from_wire(value).to_wire() == value
            continue
        if name.startswith("mutation"):
            assert MutationResult.from_wire(value).to_wire() == value
        else:
            assert QueryResult.from_wire(value).to_wire() == value
    nanosecond_result = dict(results["queryHappy"])
    nanosecond_result["receipt"] = dict(nanosecond_result["receipt"])
    nanosecond_result["receipt"]["observedAt"] = "2026-10-29T12:30:45.123456789Z"
    assert QueryResult.from_wire(nanosecond_result).to_wire() == nanosecond_result


def test_failure_and_retry_policy_conformance(conformance_dir: Path) -> None:
    """Round-trip all Go failure kinds and nanosecond retry defaults."""
    for value in json.loads((conformance_dir / "failures.json").read_text()):
        assert Failure.from_wire(value).to_wire() == value
    policy = default_connector_step_execute_retry_policy()
    assert policy.to_wire() == json.loads((conformance_dir / "retry-policy.json").read_text())
    nanosecond_wire = dict(policy.to_wire())
    nanosecond_wire["InitialInterval"] = 1_000_000_001
    assert RetryPolicy.from_wire(nanosecond_wire).to_wire() == nanosecond_wire
    defaults: dict[str, object] = {
        "executeMethodTimeout": 15_000_000_001,
        "heartbeatTimeout": 1,
        "executeDurability": 2,
    }
    assert StepDefaults.from_wire(defaults).to_wire() == defaults


def test_operation_definitions_round_trip(conformance_dir: Path) -> None:
    """Preserve Go branch optionality and nanosecond Step defaults."""
    fixture = json.loads((conformance_dir / "connectormock.json").read_text())
    query = QueryDefinition.from_wire(fixture["queryDefinition"])
    mutation = MutationDefinition.from_wire(fixture["mutationDefinition"])
    assert query.to_wire() == fixture["queryDefinition"]
    assert mutation.to_wire() == fixture["mutationDefinition"]
    assert QueryDefinition.from_wire(fixture["emptyBranches"]).to_wire() == fixture["emptyBranches"]
    assert QueryDefinition.from_wire(fixture["nullBranches"]).to_wire() == fixture["nullBranches"]


def test_configuration_ui_validation_and_secret_redaction() -> None:
    """Reject ambiguous ports and prevent secret formatting or pickling."""
    unit = ConnectorUIUnit(
        "choice",
        "picker",
        "Account",
        "Choose the account from the provider list.",
        True,
        (ConnectorUIBinding("account", "/account~1id"),),
    )
    ConnectorConfigurationUI((unit,)).validate()
    with pytest.raises(ValueError):
        ConnectorConfigurationUI((unit, unit)).validate()
    with pytest.raises(ValueError):
        ConnectorUIBinding("account", "/bad~2escape").validate()
    secret = SecretString("never-print-this")
    assert "never-print-this" not in repr(secret) + str(secret)
    with pytest.raises(TypeError):
        pickle.dumps(secret)
    reauthorization = ReauthorizationRequiredError(Exception("inert-provider-detail"))
    assert "inert-provider-detail" not in str(reauthorization) + repr(reauthorization)


def test_timestamp_attempt_validation_and_refresh_repr() -> None:
    """Preserve offsets and hide credentials while rejecting impossible outcomes."""
    observed = RFC3339NanoTimestamp.from_wire("2026-10-29T12:30:45.123456789+08:00")
    for supplied, canonical in (
        ("2026-10-29T12:30:45+00:00", "2026-10-29T12:30:45Z"),
        ("2026-10-29T12:30:45-00:00", "2026-10-29T12:30:45Z"),
        ("2026-10-29T12:30:45.120+00:00", "2026-10-29T12:30:45.12Z"),
    ):
        assert str(RFC3339NanoTimestamp.from_wire(supplied)) == canonical
    receipt = Receipt(CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf"), "sample", observed)
    assert receipt.to_wire()["observedAt"] == str(observed)
    local = Receipt(
        receipt.call_id,
        "sample",
        datetime(2026, 10, 29, 12, 30, 45, 123000, timezone(timedelta(hours=8))),
    )
    assert local.to_wire()["observedAt"] == "2026-10-29T12:30:45.123+08:00"
    failure = Failure(FailureKind.TRANSPORT, "sample", "createItem", "inert")
    with pytest.raises(ValueError):
        QueryAttempt("retry")
    with pytest.raises(ValueError):
        QueryAttempt(cast(Any, "uncertain"), "uncertain", receipt=receipt, failure=failure)
    with pytest.raises(ValueError):
        MutationAttempt("retry", failure=failure, retry_after_seconds=-1)
    for invalid_delay in (float("nan"), float("inf"), float("-inf")):
        with pytest.raises(ValueError):
            QueryAttempt.retry(failure, invalid_delay)
        with pytest.raises(ValueError):
            MutationAttempt.retry(failure, invalid_delay)
    with pytest.raises(ValueError):
        MutationResult("uncertain", None, receipt)
    with pytest.raises(ValueError):
        QueryResult("uncertain", None, receipt)
    state = CredentialRefreshState({"refreshToken": "INERT_SECRET"}, None, datetime.now(UTC))
    result = CredentialRefreshResult({"refreshToken": "INERT_SECRET"}, datetime.now(UTC))
    assert "INERT_SECRET" not in repr(state) + repr(result)


def test_failure_decoding_and_retry_input_reject_invalid_values() -> None:
    """Invalid failure kinds and negative retry hints never enter a result."""
    with pytest.raises(ValueError):
        Failure.from_wire({"kind": "UNKNOWN", "provider": "sample", "operation": "getItem", "message": "bad"})
    with pytest.raises(ValueError):
        Failure.from_wire({"kind": "VALIDATION", "provider": "sample", "operation": "getItem"})
    failure = Failure.from_wire(
        {"kind": "RATE_LIMIT", "provider": "sample", "operation": "getItem", "message": "throttled"}
    )
    with pytest.raises(ValueError):
        RetryError(failure, -1)
    with pytest.raises(ValueError):
        OperationRef("INVALID", "getItem").validate()


def test_scope_snapshot_reference_conformance(conformance_dir: Path) -> None:
    """Preserve scoped S3 keys and exact snapshot reference fields."""
    storage = json.loads((conformance_dir / "project-storage.json").read_text())
    scope = Scope.from_wire(storage["scope"])
    assert scope.prefix == "projects/sample-project/preview"
    snapshot = SnapshotRef(
        **{
            "key": storage["snapshot"]["key"],
            "version": storage["snapshot"]["version"],
            "digest": storage["snapshot"]["digest"],
            "media_type": storage["snapshot"]["mediaType"],
        }
    )
    snapshot.validate(scope)
    assert snapshot.to_wire() == storage["snapshot"]
