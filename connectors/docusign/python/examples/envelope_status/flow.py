# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read a DocuSign envelope through one durable Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.docusign import Connection, Envelope, GetEnvelopeInput, new_get_envelope_step

CONNECTION_NAME = "docusign-esignature"
FLOW_TYPE = "DocuSignEnvelopeStatusPython"
_OPERATION_STEP_TYPE = "GetDocuSignEnvelope"
_REQUEST = dex.Attribute("docusign-python-envelope-request", GetEnvelopeInput)
_ENVELOPE = dex.Attribute("docusign-python-envelope", Envelope)


def map_to_operation_input(request: GetEnvelopeInput) -> GetEnvelopeInput:
    """Pass the typed request to the provider operation."""
    return request


# dex:group group-id:docusign-envelope group-label:"DocuSign envelope"
# dex:explanation text:"Persist the envelope ID before reading DocuSign."
class _StartEnvelopeStatus(dex.Step[GetEnvelopeInput]):
    def get_step_type(self) -> str:
        return "StartDocuSignEnvelopeStatus"

    def execute(self, context: dex.Context, input: GetEnvelopeInput) -> dex.StepDecision:
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef("GetDocuSignEnvelope"), input)


# dex:group group-id:docusign-envelope group-label:"DocuSign envelope"
# dex:explanation text:"Persist the envelope status without secret or document bytes."
class _CompleteEnvelopeStatus(dex.Step[QueryResult[Envelope]]):
    def get_step_type(self) -> str:
        return "CompleteDocuSignEnvelopeStatus"

    def execute(self, context: dex.Context, input: QueryResult[Envelope]) -> dex.StepDecision:
        _ENVELOPE.set(context, input.value)
        return dex.graceful_complete(input.value)


class EnvelopeStatusFlow(dex.Flow[GetEnvelopeInput]):
    """Read one envelope through the static DocuSign connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("envelope status requires the static DocuSign connection")
        self._start = _StartEnvelopeStatus()
        self._complete = _CompleteEnvelopeStatus()
        self._get = new_get_envelope_step(
            step_type=_OPERATION_STEP_TYPE,
            step_input_type=GetEnvelopeInput,
            annotations={
                "groupId": "docusign-envelope",
                "groupLabel": "DocuSign envelope",
                "explanation": "Read the envelope status and text custom fields from DocuSign.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "DocuSignEnvelopeStatusPython"

    def get_steps(self) -> dex.StepList[GetEnvelopeInput]:
        """Register the start, provider query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._get, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register both durable, non-secret Attributes."""
        return dex.PersistenceSchema.of(_REQUEST, _ENVELOPE)
