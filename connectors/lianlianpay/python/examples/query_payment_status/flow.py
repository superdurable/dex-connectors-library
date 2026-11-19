# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable LianLianPay payment status Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.lianlianpay import (
    Connection,
    PaymentStatusResult,
    QueryPaymentStatusInput,
    new_query_payment_status_step,
)

CONNECTION_NAME = "lianlianpay"
_RESULT = dex.Attribute("lianlianpay-payment-status", PaymentStatusResult)


def map_to_operation_input(request: QueryPaymentStatusInput) -> QueryPaymentStatusInput:
    """Forward the payment status request."""
    return request


# dex:group group-id:lianlianpay group-label:"LianLianPay"
# dex:explanation text:"Start one LianLianPay payment status query."
class _StartPaymentStatus(dex.Step[QueryPaymentStatusInput]):
    def execute(self, context: dex.Context, input: QueryPaymentStatusInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallLianLianPay"), input)


# dex:group group-id:lianlianpay group-label:"LianLianPay"
# dex:explanation text:"Persist the merchant order status before completion."
class _CompletePaymentStatus(dex.Step[QueryResult[PaymentStatusResult]]):
    def execute(self, context: dex.Context, input: QueryResult[PaymentStatusResult]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class PaymentStatusFlow(dex.Flow[QueryPaymentStatusInput]):
    """Read a merchant payment status through the static LianLianPay connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("PaymentStatusFlow requires the static LianLianPay connection")
        self._start = _StartPaymentStatus()
        self._complete = _CompletePaymentStatus()
        self._call = new_query_payment_status_step(
            step_type="CallLianLianPay",
            step_input_type=QueryPaymentStatusInput,
            annotations={
                "groupId": "lianlianpay",
                "groupLabel": "LianLianPay",
                "explanation": "Read the status of one merchant payment order.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable payment status Flow identity."""
        return "LianLianPayPaymentStatus"

    def get_steps(self) -> dex.StepList[QueryPaymentStatusInput]:
        """Register the payment status Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the returned payment status only."""
        return dex.PersistenceSchema.of(_RESULT)
