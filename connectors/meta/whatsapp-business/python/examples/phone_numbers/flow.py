# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable WhatsApp Business phone-number page Flow using the listPhoneNumbers Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.meta_whatsapp_business import (
    Connection,
    ListPhoneNumbersInput,
    PhoneNumbersPage,
    new_list_phone_numbers_step,
)

FLOW_TYPE = "WhatsAppBusinessPhoneNumbersPython"
CONNECTION_NAME = "whatsapp-business"
_CALL_STEP_TYPE = "CallListPhoneNumbers"
_RESULT = dex.Attribute("whatsapp-business-python-phone-numbers", PhoneNumbersPage)


def map_to_operation_input(request: ListPhoneNumbersInput) -> ListPhoneNumbersInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:whatsapp-business group-label:"WhatsApp Business"
# dex:explanation text:"Start the WhatsApp Business request."
class _StartRequest(dex.Step[ListPhoneNumbersInput]):
    def get_step_type(self) -> str:
        return "StartListPhoneNumbers"

    def execute(self, context: dex.Context, input: ListPhoneNumbersInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListPhoneNumbers"), input)


# dex:group group-id:whatsapp-business group-label:"WhatsApp Business"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[PhoneNumbersPage]]):
    def get_step_type(self) -> str:
        return "CompleteListPhoneNumbers"

    def execute(self, context: dex.Context, input: QueryResult[PhoneNumbersPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class PhoneNumbersFlow(dex.Flow[ListPhoneNumbersInput]):
    """Run one listPhoneNumbers read through the static whatsapp-business connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the phone number Flow requires the static whatsapp-business connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_phone_numbers_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListPhoneNumbersInput,
            annotations={
                "groupId": "whatsapp-business",
                "groupLabel": "WhatsApp Business",
                "explanation": "List phone number IDs and messages using the system-user token.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "WhatsAppBusinessPhoneNumbersPython"

    def get_steps(self) -> dex.StepList[ListPhoneNumbersInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
