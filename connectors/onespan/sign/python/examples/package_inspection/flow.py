# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable OneSpanSign transaction inspection Flow using getPackage."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.onespan_sign import (
    Connection,
    GetPackageInput,
    PackageStatus,
    new_get_package_step,
)

FLOW_TYPE = "OneSpanSignPackageInspectionPython"
CONNECTION_NAME = "onespan-sign"
_CALL_STEP_TYPE = "CallOneSpanSign"
_RESULT = dex.Attribute("onespan-sign-package", PackageStatus)


def map_to_operation_input(request: GetPackageInput) -> GetPackageInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:onespan-sign group-label:"OneSpan Sign"
# dex:explanation text:"Start the OneSpan Sign request."
class _StartRequest(dex.Step[GetPackageInput]):
    def get_step_type(self) -> str:
        return "StartOneSpanSignRequest"

    def execute(self, context: dex.Context, input: GetPackageInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallOneSpanSign"), input)


# dex:group group-id:onespan-sign group-label:"OneSpan Sign"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[PackageStatus]]):
    def get_step_type(self) -> str:
        return "CompleteOneSpanSignRequest"

    def execute(self, context: dex.Context, input: QueryResult[PackageStatus]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class PackageInspectionFlow(dex.Flow[GetPackageInput]):
    """Read one transaction through the static onespan-sign connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("transaction inspection requires the static onespan-sign connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_package_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetPackageInput,
            annotations={
                "groupId": "onespan-sign",
                "groupLabel": "OneSpan Sign",
                "explanation": "Read one transaction from the connected OneSpan Sign account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "OneSpanSignPackageInspectionPython"

    def get_steps(self) -> dex.StepList[GetPackageInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
