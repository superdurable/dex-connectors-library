# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Deel contract inspection Flow using getContract."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.deel import (
    Connection,
    ContractStatus,
    GetContractInput,
    new_get_contract_step,
)

FLOW_TYPE = "DeelContractInspectionPython"
CONNECTION_NAME = "deel"
_CALL_STEP_TYPE = "CallDeel"
_RESULT = dex.Attribute("deel-contract", ContractStatus)


def map_to_operation_input(request: GetContractInput) -> GetContractInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:deel group-label:"Deel"
# dex:explanation text:"Start the Deel request."
class _StartRequest(dex.Step[GetContractInput]):
    def get_step_type(self) -> str:
        return "StartDeelRequest"

    def execute(self, context: dex.Context, input: GetContractInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallDeel"), input)


# dex:group group-id:deel group-label:"Deel"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[ContractStatus]]):
    def get_step_type(self) -> str:
        return "CompleteDeelRequest"

    def execute(self, context: dex.Context, input: QueryResult[ContractStatus]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ContractInspectionFlow(dex.Flow[GetContractInput]):
    """Read one contract through the static deel connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("contract inspection requires the static deel connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_contract_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetContractInput,
            annotations={
                "groupId": "deel",
                "groupLabel": "Deel",
                "explanation": "Read one contract from the connected Deel account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "DeelContractInspectionPython"

    def get_steps(self) -> dex.StepList[GetContractInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
