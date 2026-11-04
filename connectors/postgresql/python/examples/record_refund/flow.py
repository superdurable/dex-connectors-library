# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A runnable refund ledger Flow with a static PostgreSQL connection."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.postgresql import (
    Connection,
    ExecuteStatementInput,
    QueryRowsInput,
    RowSet,
    StatementExecution,
    new_execute_step,
    new_query_step,
)

CONNECTION_NAME = "postgresql-ledger"
REFUND_COLUMNS = "refund_id, order_id, amount_usd, external_reference, idempotency_key, recorded_at"
FIND_REFUND = f"SELECT {REFUND_COLUMNS} FROM refund_ledger WHERE order_id = $1"
INSERT_REFUND = (
    "INSERT INTO refund_ledger (order_id, amount_usd, external_reference, idempotency_key) "
    "VALUES ($1, $2::numeric, $3, $4) ON CONFLICT (idempotency_key) DO NOTHING "
    f"RETURNING {REFUND_COLUMNS}"
)


@dataclass(frozen=True, slots=True)
class RefundRequest:
    """One order and exact decimal refund amount."""

    order_id: str = field(metadata={"dex_json_name": "orderId"})
    amount_usd: str = field(metadata={"dex_json_name": "amountUsd"})
    external_reference: str = field(default="", metadata={"dex_json_name": "externalReference"})


@dataclass(frozen=True, slots=True)
class RefundOutcome:
    """The recorded or existing refund row."""

    status: str
    refund: dict[str, object]


_REQUEST = dex.Attribute("postgresql-python-refund-request", RefundRequest)
_OUTCOME = dex.Attribute("postgresql-python-refund-outcome", RefundOutcome)


# dex:group group-id:postgresql group-label:"PostgreSQL"
# dex:explanation text:"Validate the refund request before reading the ledger."
class _Start(dex.Step[RefundRequest]):
    def get_step_type(self) -> str:
        return "RecordRefundRequest"

    def execute(self, context: dex.Context, input: RefundRequest) -> dex.StepDecision:
        if not input.order_id or not input.amount_usd:
            return dex.force_fail("order ID and amount are required")
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef("FindRecordedRefund"), input)


# dex:group group-id:postgresql group-label:"PostgreSQL"
# dex:explanation text:"Reuse an existing refund or proceed to the idempotent insert."
class _Found(dex.Step[QueryResult[RowSet]]):
    def get_step_type(self) -> str:
        return "DecideRefundRecording"

    def execute(self, context: dex.Context, input: QueryResult[RowSet]) -> dex.StepDecision:
        if input.value.rows:
            outcome = RefundOutcome("alreadyRecorded", input.value.rows[0] or {})
            _OUTCOME.set(context, outcome)
            return dex.graceful_complete(outcome)
        return dex.go_to(dex.StepRef("InsertRefund"), _REQUEST.get(context))


# dex:group group-id:postgresql group-label:"PostgreSQL"
# dex:explanation text:"Complete after PostgreSQL commits the refund write."
class _Recorded(dex.Step[MutationResult[StatementExecution]]):
    def get_step_type(self) -> str:
        return "CompleteRefundRecording"

    def execute(self, context: dex.Context, input: MutationResult[StatementExecution]) -> dex.StepDecision:
        if not input.value.rows:
            return dex.force_fail("the idempotent insert returned no refund row")
        outcome = RefundOutcome("recorded", input.value.rows[0] or {})
        _OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


class RecordRefundFlow(dex.Flow[RefundRequest]):
    """Query then write an idempotent refund through PostgreSQL Steps."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("refund Flow requires the postgresql-ledger connection")
        self._start = _Start()
        self._found = _Found()
        self._recorded = _Recorded()
        self._query = new_query_step(
            step_type="FindRecordedRefund",
            step_input_type=RefundRequest,
            annotations={
                "groupId": "postgresql",
                "groupLabel": "PostgreSQL",
                "explanation": "Find the order's existing refund.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: QueryRowsInput(FIND_REFUND, (request.order_id,)),
            completed=self._found,
        )
        self._execute = new_execute_step(
            step_type="InsertRefund",
            step_input_type=RefundRequest,
            annotations={
                "groupId": "postgresql",
                "groupLabel": "PostgreSQL",
                "explanation": "Insert one refund with the Dex idempotency key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: ExecuteStatementInput(
                INSERT_REFUND,
                (request.order_id, request.amount_usd, request.external_reference),
                4,
                1,
            ),
            completed=self._recorded,
        )

    def get_flow_type(self) -> str:
        """Return the static Flow identity."""
        return "PostgreSQLPythonRecordRefund"

    def get_steps(self) -> dex.StepList[RefundRequest]:
        """Register the request and both connector Steps."""
        return dex.StepList.start_step(self._start).other_steps(
            self._query,
            self._found,
            self._execute,
            self._recorded,
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the request and final outcome."""
        return dex.PersistenceSchema.of(_REQUEST, _OUTCOME)
