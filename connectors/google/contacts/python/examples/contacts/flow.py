# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Google Contacts page Flow using the listContacts Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.google_contacts import (
    Connection,
    ContactsPage,
    ListContactsInput,
    new_list_contacts_step,
)

FLOW_TYPE = "GoogleContactsListPython"
CONNECTION_NAME = "google-contacts"
_CALL_STEP_TYPE = "CallListContacts"
_RESULT = dex.Attribute("google-contacts-python-page", ContactsPage)


def map_to_operation_input(request: ListContactsInput) -> ListContactsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:google-contacts group-label:"Google Contacts"
# dex:explanation text:"Start the Google Contacts request."
class _StartRequest(dex.Step[ListContactsInput]):
    def get_step_type(self) -> str:
        return "StartListContacts"

    def execute(self, context: dex.Context, input: ListContactsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListContacts"), input)


# dex:group group-id:google-contacts group-label:"Google Contacts"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[ContactsPage]]):
    def get_step_type(self) -> str:
        return "CompleteListContacts"

    def execute(self, context: dex.Context, input: QueryResult[ContactsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ContactsFlow(dex.Flow[ListContactsInput]):
    """Run one listContacts read through the static google-contacts connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the contacts lookup Flow requires the static google-contacts connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_contacts_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListContactsInput,
            annotations={
                "groupId": "google-contacts",
                "groupLabel": "Google Contacts",
                "explanation": "Read one Google Contacts page.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "GoogleContactsListPython"

    def get_steps(self) -> dex.StepList[ListContactsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
