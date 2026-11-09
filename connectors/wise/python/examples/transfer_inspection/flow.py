# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Wise transfer inspection Flow using getTransfer."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.wise import (
    Connection,
    GetTransferInput,
    TransferStatus,
    new_get_transfer_step,
)

FLOW_TYPE = "WiseTransferInspectionPython"
CONNECTION_NAME = "wise"
_CALL_STEP_TYPE = "CallWise"
_RESULT = dex.Attribute("wise-transfer", TransferStatus)


def map_to_operation_input(request: GetTransferInput) -> GetTransferInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:wise group-label:"Wise"
# dex:explanation text:"Start the Wise request."
class _StartRequest(dex.Step[GetTransferInput]):
    def get_step_type(self) -> str:
        return "StartWiseRequest"

    def execute(self, context: dex.Context, input: GetTransferInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallWise"), input)


# dex:group group-id:wise group-label:"Wise"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[TransferStatus]]):
    def get_step_type(self) -> str:
        return "CompleteWiseRequest"

    def execute(self, context: dex.Context, input: QueryResult[TransferStatus]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TransferInspectionFlow(dex.Flow[GetTransferInput]):
    """Read one transfer through the static wise connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("transfer inspection requires the static wise connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_transfer_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetTransferInput,
            annotations={
                "groupId": "wise",
                "groupLabel": "Wise",
                "explanation": "Read one transfer from the connected Wise account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "WiseTransferInspectionPython"

    def get_steps(self) -> dex.StepList[GetTransferInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
