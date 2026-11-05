# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Durable refund recording through the MySQL connector."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.mysql import (
    Connection,
    ExecuteStatementInput,
    QueryRowsInput,
    RowSet,
    StatementExecution,
    new_execute_step,
    new_query_step,
)

CONNECTION_NAME = "mysql-ledger"
_FIND = "SELECT refund_id, order_id, amount_usd, idempotency_key FROM refund_ledger WHERE order_id = ?"
_INSERT = (
    "INSERT INTO refund_ledger (order_id, amount_usd, external_reference, idempotency_key) "
    "VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE refund_id = LAST_INSERT_ID(refund_id)"
)


@dataclass(frozen=True, slots=True)
class RefundRequest:
    """Exact order and amount to record in the ledger."""

    order_id: str = field(metadata={"dex_json_name": "orderId"})
    amount_usd: str = field(metadata={"dex_json_name": "amountUsd"})
    external_reference: str = field(default="", metadata={"dex_json_name": "externalReference"})


@dataclass(frozen=True, slots=True)
class RefundOutcome:
    """Whether this Flow inserted a refund or found an existing order."""

    status: str
    order_id: str = field(metadata={"dex_json_name": "orderId"})


_REQUEST = dex.Attribute("mysql-python-refund-request", RefundRequest)
_OUTCOME = dex.Attribute("mysql-python-refund-outcome", RefundOutcome)
_UNCERTAIN_KEY = dex.Attribute("mysql-python-uncertain-key", str)


def _query_input(request: RefundRequest) -> QueryRowsInput:
    return QueryRowsInput(_FIND, (request.order_id,))


def _execute_input(request: RefundRequest) -> ExecuteStatementInput:
    return ExecuteStatementInput(
        _INSERT, (request.order_id, request.amount_usd, request.external_reference or None), 4, 1
    )


def _readback_input(key: str) -> QueryRowsInput:
    return QueryRowsInput(
        "SELECT order_id, idempotency_key FROM refund_ledger WHERE idempotency_key = ?", (key,)
    )


# dex:group group-id:mysql-refund group-label:"MySQL refund"
# dex:explanation text:"Persist the refund request before reading the ledger."
class _RecordRequest(dex.Step[RefundRequest]):
    def get_step_type(self) -> str:
        return "RecordRefundRequest"

    def execute(self, context: dex.Context, input: RefundRequest) -> dex.StepDecision:
        if not input.order_id.strip() or not input.amount_usd.strip():
            return dex.force_fail("orderId and amountUsd are required")
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef("FindRecordedRefund"), input)


# dex:group group-id:mysql-refund group-label:"MySQL refund"
# dex:explanation text:"Complete an existing refund or write a new one."
class _Decide(dex.Step[QueryResult[RowSet]]):
    def get_step_type(self) -> str:
        return "DecideRefund"

    def execute(self, context: dex.Context, input: QueryResult[RowSet]) -> dex.StepDecision:
        request = _REQUEST.get(context)
        if input.value.rows:
            outcome = RefundOutcome("alreadyRecorded", request.order_id)
            _OUTCOME.set(context, outcome)
            return dex.graceful_complete(outcome)
        return dex.go_to(dex.StepRef("InsertRefund"), request)


# dex:group group-id:mysql-refund group-label:"MySQL refund"
# dex:explanation text:"Complete after the idempotent insert commits."
class _Complete(dex.Step[MutationResult[StatementExecution]]):
    def get_step_type(self) -> str:
        return "CompleteRefund"

    def execute(self, context: dex.Context, input: MutationResult[StatementExecution]) -> dex.StepDecision:
        outcome = RefundOutcome("recorded", _REQUEST.get(context).order_id)
        _OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


# dex:group group-id:mysql-refund group-label:"MySQL refund"
# dex:explanation text:"Read back a write whose COMMIT outcome was unknown."
class _CheckUncertain(dex.Step[MutationResult[StatementExecution]]):
    def get_step_type(self) -> str:
        return "CheckUncertainRefund"

    def execute(self, context: dex.Context, input: MutationResult[StatementExecution]) -> dex.StepDecision:
        key = str(input.receipt.idempotency_key)
        if not key:
            return dex.force_fail("uncertain refund has no idempotency key")
        _UNCERTAIN_KEY.set(context, key)
        return dex.go_to(dex.StepRef("ReadBackUncertainRefund"), key)


# dex:group group-id:mysql-refund group-label:"MySQL refund"
# dex:explanation text:"Confirm this Step's key was committed before completing."
class _Reconcile(dex.Step[QueryResult[RowSet]]):
    def get_step_type(self) -> str:
        return "ReconcileUncertainRefund"

    def execute(self, context: dex.Context, input: QueryResult[RowSet]) -> dex.StepDecision:
        key = _UNCERTAIN_KEY.get(context)
        if (
            len(input.value.rows) != 1
            or input.value.rows[0] is None
            or input.value.rows[0]["idempotency_key"] != key
        ):
            return dex.force_fail("uncertain refund was not confirmed by read-back")
        outcome = RefundOutcome("recorded", _REQUEST.get(context).order_id)
        _OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


class RecordRefundFlow(dex.Flow[RefundRequest]):
    """Record one refund with the static mysql-ledger connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("record-refund requires the mysql-ledger connection")
        self._record = _RecordRequest()
        self._decide = _Decide()
        self._complete = _Complete()
        self._check_uncertain = _CheckUncertain()
        self._reconcile = _Reconcile()
        self._query = new_query_step(
            step_type="FindRecordedRefund",
            step_input_type=RefundRequest,
            annotations={
                "groupId": "mysql-refund",
                "groupLabel": "MySQL refund",
                "explanation": "Find an existing refund in a read-only transaction.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=_query_input,
            completed=self._decide,
        )
        self._insert = new_execute_step(
            step_type="InsertRefund",
            step_input_type=RefundRequest,
            annotations={
                "groupId": "mysql-refund",
                "groupLabel": "MySQL refund",
                "explanation": "Insert one refund with the Step idempotency key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=_execute_input,
            completed=self._complete,
            # An uncertain COMMIT requires a read-back by this Step's idempotency key.
            uncertain=self._check_uncertain,
        )
        self._readback = new_query_step(
            step_type="ReadBackUncertainRefund",
            step_input_type=str,
            annotations={
                "groupId": "mysql-refund",
                "groupLabel": "MySQL refund",
                "explanation": "Confirm an uncertain refund by its Step idempotency key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=_readback_input,
            completed=self._reconcile,
        )

    def get_flow_type(self) -> str:
        return "MySQLPythonRecordRefund"

    def get_steps(self) -> dex.StepList[RefundRequest]:
        return dex.StepList.start_step(self._record).other_steps(
            self._query,
            self._decide,
            self._insert,
            self._complete,
            self._check_uncertain,
            self._readback,
            self._reconcile,
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        return dex.PersistenceSchema.of(_REQUEST, _OUTCOME, _UNCERTAIN_KEY)
