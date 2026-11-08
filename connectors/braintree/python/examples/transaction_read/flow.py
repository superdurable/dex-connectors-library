# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Braintree transaction lookup Flow using getTransaction."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.braintree import (
    Connection,
    GetTransactionInput,
    TransactionProfile,
    new_get_transaction_step,
)

FLOW_TYPE = "BraintreeTransactionReadPython"
CONNECTION_NAME = "braintree"
_CALL_STEP_TYPE = "CallBraintree"
_RESULT = dex.Attribute("braintree-transaction-read", TransactionProfile)


def map_to_operation_input(request: GetTransactionInput) -> GetTransactionInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:braintree group-label:"Braintree"
# dex:explanation text:"Start the Braintree request."
class _StartRequest(dex.Step[GetTransactionInput]):
    def get_step_type(self) -> str:
        return "StartBraintreeRequest"

    def execute(self, context: dex.Context, input: GetTransactionInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallBraintree"), input)


# dex:group group-id:braintree group-label:"Braintree"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[TransactionProfile]]):
    def get_step_type(self) -> str:
        return "CompleteBraintreeRequest"

    def execute(self, context: dex.Context, input: QueryResult[TransactionProfile]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TransactionReadFlow(dex.Flow[GetTransactionInput]):
    """Read transaction status through the static braintree connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("transaction lookup requires the static braintree connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_transaction_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetTransactionInput,
            annotations={
                "groupId": "braintree",
                "groupLabel": "Braintree",
                "explanation": "Read transaction status from the connected Braintree workspace.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "BraintreeTransactionReadPython"

    def get_steps(self) -> dex.StepList[GetTransactionInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
