# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Paddle transaction inspection Flow using getTransaction."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.paddle import (
    Connection,
    GetTransactionInput,
    TransactionStatus,
    new_get_transaction_step,
)

FLOW_TYPE = "PaddleTransactionInspectionPython"
CONNECTION_NAME = "paddle"
_CALL_STEP_TYPE = "CallPaddle"
_RESULT = dex.Attribute("paddle-transaction", TransactionStatus)


def map_to_operation_input(request: GetTransactionInput) -> GetTransactionInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:paddle group-label:"Paddle"
# dex:explanation text:"Start the Paddle request."
class _StartRequest(dex.Step[GetTransactionInput]):
    def get_step_type(self) -> str:
        return "StartPaddleRequest"

    def execute(self, context: dex.Context, input: GetTransactionInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallPaddle"), input)


# dex:group group-id:paddle group-label:"Paddle"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[TransactionStatus]]):
    def get_step_type(self) -> str:
        return "CompletePaddleRequest"

    def execute(self, context: dex.Context, input: QueryResult[TransactionStatus]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TransactionInspectionFlow(dex.Flow[GetTransactionInput]):
    """Read one transaction through the static paddle connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("transaction inspection requires the static paddle connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_transaction_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetTransactionInput,
            annotations={
                "groupId": "paddle",
                "groupLabel": "Paddle",
                "explanation": "Read one transaction from the connected Paddle account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "PaddleTransactionInspectionPython"

    def get_steps(self) -> dex.StepList[GetTransactionInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
