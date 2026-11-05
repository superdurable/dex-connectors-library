# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Durable SQL Server refund write followed by read-back."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.microsoft_sql_server import (
    Connection,
    ExecuteStatementInput,
    QueryRowsInput,
    RowSet,
    StatementExecution,
    new_execute_step,
    new_query_step,
)

CONNECTION_NAME = "sqlserver-refunds"
FLOW_TYPE = "SQLServerRecordRefundPython"


@dataclass(frozen=True, slots=True)
class RefundRequest:
    """An order and exact decimal amount to record once."""

    order_id: str
    amount_usd: str


_REQUEST = dex.Attribute("sqlserver-python-refund-request", RefundRequest)
_RESULT = dex.Attribute("sqlserver-python-refund-result", RowSet)


def execute_input(request: RefundRequest) -> ExecuteStatementInput:
    """Use the stable Dex Call ID as the final unique write key."""
    return ExecuteStatementInput(
        "INSERT INTO dbo.refund_ledger (order_id, amount_usd, call_key) "
        "SELECT @p1, CAST(@p2 AS decimal(12,2)), @p3 "
        "WHERE NOT EXISTS (SELECT 1 FROM dbo.refund_ledger WHERE call_key = @p3)",
        (request.order_id, request.amount_usd),
        idempotency_key_placeholder=3,
        max_rows_affected=1,
    )


def query_input(request: RefundRequest) -> QueryRowsInput:
    """Read back the committed refund by its application order ID."""
    return QueryRowsInput(
        "SELECT order_id, amount_usd, call_key FROM dbo.refund_ledger WHERE order_id = @p1",
        (request.order_id,),
    )


# dex:group group-id:refund group-label:"Record refund"
# dex:explanation text:"Validate and retain the refund request before the SQL Server write."
class PrepareRefund(dex.Step[RefundRequest]):
    def get_step_type(self) -> str:
        return "PrepareRefund"

    def execute(self, context: dex.Context, input: RefundRequest) -> dex.StepDecision:
        if not input.order_id.strip() or not input.amount_usd.strip():
            return dex.force_fail("order_id and amount_usd are required")
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef("WriteRefund"), input)


# dex:group group-id:refund group-label:"Record refund"
# dex:explanation text:"Pass the retained order to the read-back Query after the write commits."
class PrepareReadBack(dex.Step[MutationResult[StatementExecution]]):
    def get_step_type(self) -> str:
        return "PrepareReadBack"

    def execute(self, context: dex.Context, input: MutationResult[StatementExecution]) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ReadRefund"), _REQUEST.get(context))


# dex:group group-id:refund group-label:"Record refund"
# dex:explanation text:"Persist the read-back rows and complete the refund Flow."
class CompleteRefund(dex.Step[QueryResult[RowSet]]):
    def get_step_type(self) -> str:
        return "CompleteRefund"

    def execute(self, context: dex.Context, input: QueryResult[RowSet]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class RecordRefundFlow(dex.Flow[RefundRequest]):
    """Write a refund once and read its committed row through SQL Server."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("refund Flow requires its static SQL Server connection")
        self.prepare = PrepareRefund()
        self.prepare_read = PrepareReadBack()
        self.complete = CompleteRefund()
        self.execute = new_execute_step(
            step_type="WriteRefund",
            step_input_type=RefundRequest,
            annotations={
                "groupId": "refund",
                "groupLabel": "Record refund",
                "explanation": "Insert the keyed refund transaction once.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=execute_input,
            completed=self.prepare_read,
        )
        self.query = new_query_step(
            step_type="ReadRefund",
            step_input_type=RefundRequest,
            annotations={
                "groupId": "refund",
                "groupLabel": "Record refund",
                "explanation": "Read the committed refund row by order ID.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=query_input,
            completed=self.complete,
        )

    def get_flow_type(self) -> str:
        return "SQLServerRecordRefundPython"

    def get_steps(self) -> dex.StepList[RefundRequest]:
        return dex.StepList.start_step(self.prepare).other_steps(
            self.execute, self.prepare_read, self.query, self.complete
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        return dex.PersistenceSchema.of(_REQUEST, _RESULT)
