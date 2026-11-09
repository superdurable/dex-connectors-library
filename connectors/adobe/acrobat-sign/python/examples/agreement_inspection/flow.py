# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable AcrobatSign agreement inspection Flow using getAgreement."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.adobe_acrobat_sign import (
    AgreementStatus,
    Connection,
    GetAgreementInput,
    new_get_agreement_step,
)

FLOW_TYPE = "AcrobatSignAgreementInspectionPython"
CONNECTION_NAME = "adobe-acrobat-sign"
_CALL_STEP_TYPE = "CallAcrobatSign"
_RESULT = dex.Attribute("adobe-acrobat-sign-agreement", AgreementStatus)


def map_to_operation_input(request: GetAgreementInput) -> GetAgreementInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:adobe-acrobat-sign group-label:"AcrobatSign"
# dex:explanation text:"Start the AcrobatSign request."
class _StartRequest(dex.Step[GetAgreementInput]):
    def get_step_type(self) -> str:
        return "StartAcrobatSignRequest"

    def execute(self, context: dex.Context, input: GetAgreementInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallAcrobatSign"), input)


# dex:group group-id:adobe-acrobat-sign group-label:"AcrobatSign"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[AgreementStatus]]):
    def get_step_type(self) -> str:
        return "CompleteAcrobatSignRequest"

    def execute(self, context: dex.Context, input: QueryResult[AgreementStatus]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class AgreementInspectionFlow(dex.Flow[GetAgreementInput]):
    """Read one agreement through the static adobe-acrobat-sign connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("agreement inspection requires the static adobe-acrobat-sign connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_agreement_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetAgreementInput,
            annotations={
                "groupId": "adobe-acrobat-sign",
                "groupLabel": "AcrobatSign",
                "explanation": "Read one agreement from the connected AcrobatSign account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "AcrobatSignAgreementInspectionPython"

    def get_steps(self) -> dex.StepList[GetAgreementInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
