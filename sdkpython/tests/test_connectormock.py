# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider mock script precedence, replay, and fixture decoding tests."""

import json
from pathlib import Path

import pytest

from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    Failure,
    FailureKind,
    MutationDefinition,
    OperationRef,
    QueryDefinition,
)
from dex_connectors.sdk.connectormock import (
    Case,
    Mutation,
    Query,
    RecordedCall,
    ScriptedOperation,
    _append_to_report,
    load_fixture,
)


@pytest.mark.asyncio
async def test_operation_adapters_use_real_definitions_and_require_step_context(
    conformance_dir: Path,
) -> None:
    """Expose Go-compatible mocks without silently inventing Dex context data."""
    fixture = json.loads((conformance_dir / "connectormock.json").read_text())
    query_definition = QueryDefinition.from_wire(fixture["queryDefinition"])
    mutation_definition = MutationDefinition.from_wire(fixture["mutationDefinition"])
    query = Query[dict[str, str], str](query_definition).respond(Case.branch_result("found", "ok"))
    mutation = Mutation[dict[str, str], str](mutation_definition).respond(
        Case.branch_result("created", "ok")
    )
    assert query.definition() == query_definition
    assert mutation.definition() == mutation_definition
    call_id = CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf")
    assert mutation.idempotency_key(call_id, {}) == str(call_id)
    call = Call(call_id, ConnectionRef("sample", "main"), query_definition.operation)
    with pytest.raises(ValueError, match="live Dex Step context"):
        await query.invoke(call, {})


def test_flow_precedence_retry_and_terminal_key_replay() -> None:
    """Flow-specific cases win, retries consume the next case, and keyed results replay."""
    operation = OperationRef("sample", "createItem")
    mock = ScriptedOperation[dict[str, str], str](operation, ("created", "defect", "uncertain"), True)
    retry: Case[str] = Case.retry(Failure(FailureKind.RATE_LIMIT, "sample", "createItem", "throttled"))
    mock.default(Case.branch_result("created", "default"))
    mock.for_flow("flow-special").respond(retry, Case.branch_result("created", "special"))
    connection = ConnectionRef("sample", "main")
    assert mock.answer("flow-special-1", "call", connection, {}, "key").kind == "retry"
    assert mock.answer("flow-special-1", "call", connection, {}, "key").value == "special"
    assert mock.answer("flow-special-1", "call", connection, {}, "key").value == "special"
    assert mock.calls()[-1].replay
    assert mock.answer("flow-other", "call-2", connection, {}, "other").value == "default"


def test_strict_fixture_loader_and_go_recorded_call(conformance_dir: Path, tmp_path: Path) -> None:
    """Reject duplicate fixture keys and preserve the Go recorded-call projection."""
    fixture = json.loads((conformance_dir / "connectormock.json").read_text())
    mock = ScriptedOperation[dict[str, str], str](OperationRef("sample", "createItem"), ("created", "defect"))
    mock.respond(Case.branch_result("created", "ok"))
    mock.answer("flow-42", fixture["recordedCall"]["callId"], ConnectionRef("sample", "main"), {})
    assert mock.calls()[0].to_wire() == fixture["recordedCall"]
    duplicate = tmp_path / "duplicate.json"
    duplicate.write_text('{"a":1,"a":2}')
    with pytest.raises(ValueError):
        load_fixture(duplicate)


def test_uncertain_only_for_declared_mutation() -> None:
    """A Query cannot script an uncertain terminal route."""
    failure = Failure(FailureKind.TRANSPORT, "sample", "createItem", "outcome unknown")
    mock = ScriptedOperation[dict[str, str], str](OperationRef("sample", "getItem"), ("found", "defect"))
    with pytest.raises(ValueError):
        mock.respond(Case.uncertain("", failure))


def test_strict_unscripted_and_invalid_branch() -> None:
    """An unscripted strict call selects defect and records its omission."""
    mock = ScriptedOperation[dict[str, str], str](OperationRef("sample", "getItem"), ("found", "defect"))
    mock.default(Case.branch_result("found", "fallback")).strict()
    result = mock.answer("flow-1", "call-1", ConnectionRef("sample", "main"), {})
    assert result.branch == "defect"
    assert mock.calls()[0].unscripted
    assert mock.failures()
    with pytest.raises(ValueError):
        mock.respond(Case.branch_result("undeclared", "value"))


def test_paged_case_rejects_out_of_range_page() -> None:
    """An invalid page reports a failure and selects a defect branch."""
    mock = ScriptedOperation[int, str](OperationRef("sample", "listItems"), ("found", "defect"))
    mock.respond(Case.paged("found", ("first", "second")))
    assert (
        mock.answer("flow-1", "call-1", ConnectionRef("sample", "main"), 3, page_number=3).branch == "defect"
    )
    assert mock.failures()
    with pytest.raises(AssertionError):
        mock.assert_no_failures()


def test_go_report_and_recorded_flags(
    conformance_dir: Path, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """Append report rows and preserve Go retry, replay, and unscripted flags."""
    fixture = json.loads((conformance_dir / "connectormock.json").read_text())
    assert fixture["reportDirectoryEnvironment"] == "DEX_CONNECTOR_MOCK_REPORT_DIR"
    assert fixture["reportFileName"] == "connector-calls.json"
    monkeypatch.setenv(fixture["reportDirectoryEnvironment"], str(tmp_path))
    for key, flag in (
        ("retryRecordedCall", "retry"),
        ("replayedRecordedCall", "replay"),
        ("unscriptedRecordedCall", "unscripted"),
    ):
        value = fixture[key]
        recorded: RecordedCall[dict[str, str]] = RecordedCall(
            value["connectorId"],
            value["operationId"],
            value["connectionName"],
            value["flowId"],
            value["callId"],
            {},
            value.get("branch", ""),
            flag == "retry",
            flag == "replay",
            flag == "unscripted",
        )
        assert recorded.to_wire() == value
    rows = fixture["reportFile"]["calls"]
    for row in rows:
        recorded = RecordedCall(
            row["connectorId"],
            row["operation"],
            row["connection"],
            row["flowId"],
            row["callId"],
            {},
            row.get("branch", ""),
            row.get("retry", False),
            row.get("replay", False),
            row.get("unscripted", False),
        )
        assert recorded.to_report_wire() == row
        _append_to_report(recorded)
    report_bytes = (tmp_path / fixture["reportFileName"]).read_bytes()
    assert b"\\u003c\\u0026\\u003e" in report_bytes
    report = json.loads(report_bytes)
    assert report == fixture["reportFile"]
    assert (
        fixture["report"]
        == RecordedCall(
            "sample", "createItem", "main", "flow-42", rows[0]["callId"], {}, "created"
        ).to_report_wire()
    )
    (tmp_path / fixture["reportFileName"]).write_bytes(b'{"calls":[]}\n')
    with pytest.raises(ValueError):
        _append_to_report(recorded)
