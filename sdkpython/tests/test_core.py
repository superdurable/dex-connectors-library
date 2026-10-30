# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Identity, result, failure, retry, configuration UI, and credential tests."""

import json
import pickle
from pathlib import Path

import pytest

from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    Failure,
    MutationDefinition,
    MutationResult,
    OperationRef,
    QueryDefinition,
    QueryResult,
    ReauthorizationRequiredError,
    RetryError,
    RetryPolicy,
    SecretString,
    StepDefaults,
    default_connector_step_execute_retry_policy,
    derive_call_id,
)
from dex_connectors.sdk.projectconfig.models import Scope, SnapshotRef


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
    results = json.loads((conformance_dir / "results.json").read_text())
    for name, value in results.items():
        decoded: QueryResult[object] = (
            MutationResult if name.startswith("mutation") else QueryResult
        ).from_wire(value)
        assert decoded.to_wire() == value
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
