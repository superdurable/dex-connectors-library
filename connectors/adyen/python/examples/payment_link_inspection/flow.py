# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Adyen payment link inspection Flow using getPaymentLink."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.adyen import (
    Connection,
    GetPaymentLinkInput,
    PaymentLinkStatus,
    new_get_payment_link_step,
)

FLOW_TYPE = "AdyenPaymentLinkInspectionPython"
CONNECTION_NAME = "adyen"
_CALL_STEP_TYPE = "CallAdyen"
_RESULT = dex.Attribute("adyen-payment-link", PaymentLinkStatus)


def map_to_operation_input(request: GetPaymentLinkInput) -> GetPaymentLinkInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:adyen group-label:"Adyen"
# dex:explanation text:"Start the Adyen request."
class _StartRequest(dex.Step[GetPaymentLinkInput]):
    def get_step_type(self) -> str:
        return "StartAdyenRequest"

    def execute(self, context: dex.Context, input: GetPaymentLinkInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallAdyen"), input)


# dex:group group-id:adyen group-label:"Adyen"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[PaymentLinkStatus]]):
    def get_step_type(self) -> str:
        return "CompleteAdyenRequest"

    def execute(self, context: dex.Context, input: QueryResult[PaymentLinkStatus]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class PaymentLinkInspectionFlow(dex.Flow[GetPaymentLinkInput]):
    """Read one payment link through the static adyen connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("payment link inspection requires the static adyen connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_payment_link_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetPaymentLinkInput,
            annotations={
                "groupId": "adyen",
                "groupLabel": "Adyen",
                "explanation": "Read one payment link from the connected Adyen account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "AdyenPaymentLinkInspectionPython"

    def get_steps(self) -> dex.StepList[GetPaymentLinkInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
