# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider mock script precedence, replay, and fixture decoding tests."""

import json
from pathlib import Path

import pytest

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind, OperationRef
from dex_connectors.sdk.connectormock import Case, ScriptedOperation, load_fixture


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
    with pytest.raises(ValueError):
        mock.respond(Case.branch_result("undeclared", "value"))


def test_paged_case_rejects_out_of_range_page() -> None:
    """A page number outside the scripted listing cannot invent provider data."""
    mock = ScriptedOperation[int, str](OperationRef("sample", "listItems"), ("found", "defect"))
    mock.respond(Case.paged("found", ("first", "second")))
    with pytest.raises(ValueError):
        mock.answer("flow-1", "call-1", ConnectionRef("sample", "main"), 3, page_number=3)
