# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Amazon SES identity lookup Flow using getEmailIdentity."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.amazon_ses import (
    Connection,
    EmailIdentityDetails,
    GetEmailIdentityInput,
    new_get_email_identity_step,
)

FLOW_TYPE = "AmazonSESIdentityLookupPython"
CONNECTION_NAME = "amazon-ses"
_CALL_STEP_TYPE = "CallGetEmailIdentity"
_RESULT = dex.Attribute("ses-identity-lookup", EmailIdentityDetails)


def map_to_operation_input(request: GetEmailIdentityInput) -> GetEmailIdentityInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:ses group-label:"Amazon SES"
# dex:explanation text:"Start the Amazon SES request."
class _StartRequest(dex.Step[GetEmailIdentityInput]):
    def get_step_type(self) -> str:
        return "StartGetEmailIdentityRequest"

    def execute(self, context: dex.Context, input: GetEmailIdentityInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetEmailIdentity"), input)


# dex:group group-id:ses group-label:"Amazon SES"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[EmailIdentityDetails]]):
    def get_step_type(self) -> str:
        return "CompleteGetEmailIdentityRequest"

    def execute(self, context: dex.Context, input: QueryResult[EmailIdentityDetails]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class IdentityLookupFlow(dex.Flow[GetEmailIdentityInput]):
    """Read one identity metadata through the static amazon-ses connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("identity lookup requires the static amazon-ses connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_email_identity_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetEmailIdentityInput,
            annotations={
                "groupId": "ses",
                "groupLabel": "Amazon SES",
                "explanation": "Read one identity metadata from the connected Amazon SES account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "AmazonSESIdentityLookupPython"

    def get_steps(self) -> dex.StepList[GetEmailIdentityInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
