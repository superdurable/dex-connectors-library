# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Wave customer lookup Flow using getCustomer."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.wave import (
    Connection,
    CustomerProfile,
    GetCustomerInput,
    new_get_customer_step,
)

FLOW_TYPE = "WaveCustomerReadPython"
CONNECTION_NAME = "wave"
_CALL_STEP_TYPE = "CallWave"
_RESULT = dex.Attribute("wave-customer-read", CustomerProfile)


def map_to_operation_input(request: GetCustomerInput) -> GetCustomerInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:wave group-label:"Wave"
# dex:explanation text:"Start the Wave request."
class _StartRequest(dex.Step[GetCustomerInput]):
    def get_step_type(self) -> str:
        return "StartWaveRequest"

    def execute(self, context: dex.Context, input: GetCustomerInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallWave"), input)


# dex:group group-id:wave group-label:"Wave"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[CustomerProfile]]):
    def get_step_type(self) -> str:
        return "CompleteWaveRequest"

    def execute(self, context: dex.Context, input: QueryResult[CustomerProfile]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CustomerReadFlow(dex.Flow[GetCustomerInput]):
    """Read customer profile through the static wave connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("customer lookup requires the static wave connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_customer_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetCustomerInput,
            annotations={
                "groupId": "wave",
                "groupLabel": "Wave",
                "explanation": "Read customer profile from the connected Wave workspace.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "WaveCustomerReadPython"

    def get_steps(self) -> dex.StepList[GetCustomerInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
