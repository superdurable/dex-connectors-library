# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Dropbox Sign signature-request inspection Flow using getSignatureRequest."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.dropbox_sign import (
    Connection,
    GetSignatureRequestInput,
    SignatureRequestStatus,
    new_get_signature_request_step,
)

FLOW_TYPE = "DropboxSignSignatureRequestInspectionPython"
CONNECTION_NAME = "dropbox-sign"
_CALL_STEP_TYPE = "CallDropboxSign"
_RESULT = dex.Attribute("dropbox-sign-signature-request", SignatureRequestStatus)


def map_to_operation_input(request: GetSignatureRequestInput) -> GetSignatureRequestInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:dropbox-sign group-label:"Dropbox Sign"
# dex:explanation text:"Start the Dropbox Sign request."
class _StartRequest(dex.Step[GetSignatureRequestInput]):
    def get_step_type(self) -> str:
        return "StartDropboxSignRequest"

    def execute(self, context: dex.Context, input: GetSignatureRequestInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallDropboxSign"), input)


# dex:group group-id:dropbox-sign group-label:"Dropbox Sign"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[SignatureRequestStatus]]):
    def get_step_type(self) -> str:
        return "CompleteDropboxSignRequest"

    def execute(self, context: dex.Context, input: QueryResult[SignatureRequestStatus]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class SignatureRequestInspectionFlow(dex.Flow[GetSignatureRequestInput]):
    """Read one signature request through the static dropbox-sign connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("signature request inspection requires the static dropbox-sign connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_signature_request_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetSignatureRequestInput,
            annotations={
                "groupId": "dropbox-sign",
                "groupLabel": "Dropbox Sign",
                "explanation": "Read one signature request from the connected Dropbox Sign account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "DropboxSignSignatureRequestInspectionPython"

    def get_steps(self) -> dex.StepList[GetSignatureRequestInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
