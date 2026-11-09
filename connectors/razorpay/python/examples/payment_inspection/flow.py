# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Razorpay payment inspection Flow using getPayment."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.razorpay import (
    Connection,
    GetPaymentInput,
    PaymentStatus,
    new_get_payment_step,
)

FLOW_TYPE = "RazorpayPaymentInspectionPython"
CONNECTION_NAME = "razorpay"
_CALL_STEP_TYPE = "CallRazorpay"
_RESULT = dex.Attribute("razorpay-payment", PaymentStatus)


def map_to_operation_input(request: GetPaymentInput) -> GetPaymentInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:razorpay group-label:"Razorpay"
# dex:explanation text:"Start the Razorpay request."
class _StartRequest(dex.Step[GetPaymentInput]):
    def get_step_type(self) -> str:
        return "StartRazorpayRequest"

    def execute(self, context: dex.Context, input: GetPaymentInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallRazorpay"), input)


# dex:group group-id:razorpay group-label:"Razorpay"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[PaymentStatus]]):
    def get_step_type(self) -> str:
        return "CompleteRazorpayRequest"

    def execute(self, context: dex.Context, input: QueryResult[PaymentStatus]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class PaymentInspectionFlow(dex.Flow[GetPaymentInput]):
    """Read one payment through the static razorpay connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("payment inspection requires the static razorpay connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_payment_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetPaymentInput,
            annotations={
                "groupId": "razorpay",
                "groupLabel": "Razorpay",
                "explanation": "Read one payment from the connected Razorpay account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "RazorpayPaymentInspectionPython"

    def get_steps(self) -> dex.StepList[GetPaymentInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
