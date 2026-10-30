# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Scriptable provider outcomes and strict JSON fixtures for connector tests."""

import json
import os
import threading
import time
from collections.abc import Callable
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Generic, TypeVar, cast

from ..errors import Failure, FailureKind
from ..types import DEFECT_BRANCH_ID, UNCERTAIN_BRANCH_ID, ConnectionRef, OperationRef

Input = TypeVar("Input")
Output = TypeVar("Output")
REPORT_DIRECTORY_ENVIRONMENT = "DEX_CONNECTOR_MOCK_REPORT_DIR"
REPORT_FILE_NAME = "connector-calls.json"
_REPORT_LOCK = threading.Lock()


@dataclass(frozen=True, slots=True)
class Case(Generic[Output]):
    """One scripted branch, retry, uncertain outcome, or listing page."""

    kind: str
    branch: str = ""
    value: Output | None = None
    failure: Failure | None = None
    retry_after_seconds: float = 0
    pages: tuple[Output, ...] = ()

    @classmethod
    def branch_result(cls, branch: str, value: Output, failure: Failure | None = None) -> "Case[Output]":
        """Answer a definite declared branch."""
        return cls("branch", branch, value, failure)

    @classmethod
    def retry(cls, failure: Failure, retry_after_seconds: float = 0) -> "Case[Output]":
        """Request another Step attempt after an optional provider delay."""
        return cls("retry", failure=failure, retry_after_seconds=retry_after_seconds)

    @classmethod
    def uncertain(cls, value: Output, failure: Failure) -> "Case[Output]":
        """Answer a nonrepeatable, unconfirmable possibly accepted mutation."""
        return cls("uncertain", UNCERTAIN_BRANCH_ID, value, failure)

    @classmethod
    def paged(cls, branch: str, pages: tuple[Output, ...]) -> "Case[Output]":
        """Answer successive pages of a listing by one-based page number."""
        if not pages:
            raise ValueError("paged mock requires at least one page")
        return cls("paged", branch, pages=pages)


@dataclass(frozen=True, slots=True)
class RecordedCall(Generic[Input]):
    """One answered mock call, including replay and retry classification."""

    connector_id: str
    operation_id: str
    connection_name: str
    flow_id: str
    call_id: str
    input: Input
    branch: str = ""
    retry: bool = False
    replay: bool = False
    unscripted: bool = False

    def to_wire(self) -> dict[str, object]:
        """Encode the process-level Go RecordedCall JSON shape."""
        result: dict[str, object] = {
            "connectorId": self.connector_id,
            "operationId": self.operation_id,
            "connectionName": self.connection_name,
            "flowId": self.flow_id,
            "callId": self.call_id,
        }
        if self.branch:
            result["branch"] = self.branch
        if self.retry:
            result["retry"] = True
        if self.replay:
            result["replay"] = True
        if self.unscripted:
            result["unscripted"] = True
        return result

    def to_report_wire(self) -> dict[str, object]:
        """Encode the Go connector report row without private input values."""
        result: dict[str, object] = {
            "connection": self.connection_name,
            "operation": self.operation_id,
        }
        if self.branch:
            result["branch"] = self.branch
        result["connectorId"] = self.connector_id
        if self.flow_id:
            result["flowId"] = self.flow_id
        if self.call_id:
            result["callId"] = self.call_id
        if self.retry:
            result["retry"] = True
        if self.replay:
            result["replay"] = True
        if self.unscripted:
            result["unscripted"] = True
        return result


def _append_to_report(call: RecordedCall[Any]) -> None:
    directory = os.environ.get(REPORT_DIRECTORY_ENVIRONMENT, "")
    if not directory:
        return
    report_path = Path(directory) / REPORT_FILE_NAME
    lock_path = report_path.with_name(report_path.name + ".lock")
    with _REPORT_LOCK:
        deadline = time.monotonic() + 30
        while True:
            try:
                descriptor = os.open(lock_path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
                os.close(descriptor)
                break
            except FileExistsError:
                try:
                    if time.time() - lock_path.stat().st_mtime > 10:
                        lock_path.unlink(missing_ok=True)
                        continue
                except FileNotFoundError:
                    continue
                if time.monotonic() >= deadline:
                    raise OSError("connector call report lock stayed held") from None
                time.sleep(0.005)
        try:
            if report_path.exists():
                document = json.loads(report_path.read_text())
                if not isinstance(document, dict) or not isinstance(document.get("calls"), list):
                    raise ValueError("connector call report is invalid")
            else:
                document = {"calls": []}
            document["calls"].append(call.to_report_wire())
            descriptor = os.open(report_path, os.O_CREAT | os.O_WRONLY | os.O_TRUNC, 0o600)
            with os.fdopen(descriptor, "w") as destination:
                json.dump(document, destination, ensure_ascii=False, separators=(",", ":"))
        finally:
            lock_path.unlink(missing_ok=True)


@dataclass(slots=True)
class _Script(Generic[Input, Output]):
    rules: list[tuple[Callable[[Input], bool], Case[Output]]] = field(default_factory=list)
    queue: list[Case[Output]] = field(default_factory=list)
    defaults: list[Case[Output]] = field(default_factory=list)
    default_count: int = 0

    def answer(self, input_value: Input) -> Case[Output] | None:
        for predicate, response in self.rules:
            if predicate(input_value):
                return response
        return self.queue.pop(0) if self.queue else None

    def default(self) -> Case[Output] | None:
        if not self.defaults:
            return None
        result = self.defaults[min(self.default_count, len(self.defaults) - 1)]
        self.default_count += 1
        return result


class FlowScript(Generic[Input, Output]):
    """Cases scoped to one Flow ID prefix, preceding an operation's own script."""

    def __init__(self, prefix: str) -> None:
        if not prefix.strip():
            raise ValueError("Flow script prefix is required")
        self.prefix = prefix
        self._script: _Script[Input, Output] = _Script()

    def respond(self, *cases: Case[Output]) -> "FlowScript[Input, Output]":
        """Queue cases that answer the next matching calls."""
        self._script.queue.extend(cases)
        return self

    def when(self, predicate: Callable[[Input], bool], response: Case[Output]) -> "FlowScript[Input, Output]":
        """Answer every matching input with one reusable case."""
        self._script.rules.append((predicate, response))
        return self

    def default(self, *cases: Case[Output]) -> "FlowScript[Input, Output]":
        """Set fallback cases, repeating the last after exhaustion."""
        self._script.defaults = list(cases)
        self._script.default_count = 0
        return self


class ScriptedOperation(Generic[Input, Output]):
    """Thread-safe Query or Mutation test model without Dex Step registration."""

    def __init__(
        self,
        operation: OperationRef,
        branches: tuple[str, ...],
        is_mutation: bool = False,
        report_failure: Callable[[str], None] | None = None,
    ) -> None:
        operation.validate()
        if DEFECT_BRANCH_ID not in branches:
            raise ValueError("mock operation must declare defect branch")
        self.operation = operation
        self.branches = frozenset(branches)
        self.is_mutation = is_mutation
        self._script: _Script[Input, Output] = _Script()
        self._flow_scripts: dict[str, FlowScript[Input, Output]] = {}
        self._strict = False
        self._terminal_by_key: dict[str, Case[Output]] = {}
        self._calls: list[RecordedCall[Input]] = []
        self._failures: list[str] = []
        self._report_failure_callback = report_failure
        self._lock = threading.RLock()

    def respond(self, *cases: Case[Output]) -> "ScriptedOperation[Input, Output]":
        """Queue cases for unscoped calls."""
        self._validate_cases(cases)
        with self._lock:
            self._script.queue.extend(cases)
        return self

    def when(
        self, predicate: Callable[[Input], bool], response: Case[Output]
    ) -> "ScriptedOperation[Input, Output]":
        """Add an unscoped reusable input rule."""
        self._validate_cases((response,))
        with self._lock:
            self._script.rules.append((predicate, response))
        return self

    def default(self, *cases: Case[Output]) -> "ScriptedOperation[Input, Output]":
        """Set fallback cases, repeating the final one."""
        self._validate_cases(cases)
        with self._lock:
            self._script.defaults = list(cases)
            self._script.default_count = 0
        return self

    def strict(self) -> "ScriptedOperation[Input, Output]":
        """Require a Respond or When answer for every call."""
        with self._lock:
            self._strict = True
        return self

    def for_flow(self, flow_id_or_prefix: str) -> FlowScript[Input, Output]:
        """Return the script for a Flow ID prefix, creating it once."""
        with self._lock:
            if flow_id_or_prefix not in self._flow_scripts:
                self._flow_scripts[flow_id_or_prefix] = FlowScript(flow_id_or_prefix)
            return self._flow_scripts[flow_id_or_prefix]

    def answer(
        self,
        flow_id: str,
        call_id: str,
        connection: ConnectionRef,
        input_value: Input,
        idempotency_key: str = "",
        page_number: int = 1,
    ) -> Case[Output]:
        """Resolve script precedence and replay terminal keyed Mutation results."""
        with self._lock:
            replay = bool(idempotency_key and idempotency_key in self._terminal_by_key)
            unscripted = False
            response: Case[Output] | None
            if replay:
                response = self._terminal_by_key[idempotency_key]
            else:
                scoped = [
                    item._script
                    for _, item in sorted(
                        (
                            (prefix, script)
                            for prefix, script in self._flow_scripts.items()
                            if flow_id.startswith(prefix)
                        ),
                        key=lambda entry: -len(entry[0]),
                    )
                ]
                scripts = scoped + [self._script]
                response = next(
                    (answer for script in scripts if (answer := script.answer(input_value)) is not None), None
                )
                if response is None and not self._strict:
                    response = next(
                        (answer for script in scripts if (answer := script.default()) is not None), None
                    )
                if response is None:
                    unscripted = True
                    self._report_failure(f"unscripted call for Flow {flow_id!r}")
                    response = Case.branch_result(
                        DEFECT_BRANCH_ID,
                        cast(Output, None),
                        Failure(
                            FailureKind.LOCAL_DEFECT,
                            self.operation.connector_id,
                            self.operation.operation_id,
                            "unscripted connector mock call",
                        ),
                    )
                if response.kind == "paged":
                    if page_number < 1 or page_number > len(response.pages):
                        self._report_failure("requested page is outside the scripted listing")
                        response = Case.branch_result(
                            DEFECT_BRANCH_ID,
                            cast(Output, None),
                            Failure(
                                FailureKind.LOCAL_DEFECT,
                                self.operation.connector_id,
                                self.operation.operation_id,
                                "requested page is outside the scripted listing",
                            ),
                        )
                    else:
                        response = Case.branch_result(response.branch, response.pages[page_number - 1])
                if idempotency_key and response.kind != "retry":
                    self._terminal_by_key[idempotency_key] = response
            recorded = RecordedCall(
                self.operation.connector_id,
                self.operation.operation_id,
                connection.name,
                flow_id,
                call_id,
                input_value,
                "" if response.kind == "retry" else response.branch,
                response.kind == "retry",
                replay,
                unscripted,
            )
            self._calls.append(recorded)
            try:
                _append_to_report(recorded)
            except (OSError, ValueError) as error:
                self._report_failure(str(error))
            return response

    def calls(self) -> tuple[RecordedCall[Input], ...]:
        """Return answered calls in answer order."""
        with self._lock:
            return tuple(self._calls)

    def failures(self) -> tuple[str, ...]:
        """Return missing scripts and report failures for test assertions."""
        with self._lock:
            return tuple(self._failures)

    def assert_no_failures(self) -> None:
        """Fail the consuming test when mock calls were unscripted or invalid."""
        failures = self.failures()
        if failures:
            raise AssertionError("; ".join(failures))

    def _report_failure(self, message: str) -> None:
        self._failures.append(message)
        if self._report_failure_callback is not None:
            self._report_failure_callback(message)

    def _validate_cases(self, cases: tuple[Case[Output], ...]) -> None:
        for response in cases:
            if response.kind in ("branch", "paged") and response.branch not in self.branches:
                raise ValueError("mock branch is not declared")
            if response.kind == "uncertain" and (
                not self.is_mutation or UNCERTAIN_BRANCH_ID not in self.branches
            ):
                raise ValueError("uncertainty requires a declared Mutation branch")
            if response.kind == "retry" and (response.failure is None or response.retry_after_seconds < 0):
                raise ValueError("invalid mock retry")
            if response.failure is not None:
                response.failure.validate()


def load_fixture(path: str | Path, allowed_fields: set[str] | None = None) -> dict[str, Any]:
    """Load one JSON object, rejecting duplicate and unknown top-level fields."""

    def decode_pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
        result: dict[str, Any] = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("fixture has duplicate JSON fields")
            result[key] = value
        return result

    with Path(path).open("rb") as source:
        document = json.load(source, object_pairs_hook=decode_pairs)
    if not isinstance(document, dict):
        raise ValueError("fixture must be a JSON object")
    if allowed_fields is not None and not set(document) <= allowed_fields:
        raise ValueError("fixture has unknown fields")
    return document


__all__ = ["Case", "FlowScript", "RecordedCall", "ScriptedOperation", "load_fixture"]
