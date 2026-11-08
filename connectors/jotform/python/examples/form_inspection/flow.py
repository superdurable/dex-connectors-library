# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Jotform form inspection Flow using the listForms Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.jotform import (
    Connection,
    FormsPage,
    ListFormsInput,
    new_list_forms_step,
)

FLOW_TYPE = "JotformFormInspection"
CONNECTION_NAME = "jotform"
_CALL_STEP_TYPE = "CallListForms"
_RESULT = dex.Attribute("jotform-forms-page", FormsPage)


def map_to_operation_input(request: ListFormsInput) -> ListFormsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:jotform group-label:"Jotform"
# dex:explanation text:"Start the Jotform request."
class _StartRequest(dex.Step[ListFormsInput]):
    def get_step_type(self) -> str:
        return "StartListForms"

    def execute(self, context: dex.Context, input: ListFormsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListForms"), input)


# dex:group group-id:jotform group-label:"Jotform"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[FormsPage]]):
    def get_step_type(self) -> str:
        return "CompleteListForms"

    def execute(self, context: dex.Context, input: QueryResult[FormsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class FormInspectionFlow(dex.Flow[ListFormsInput]):
    """Run one listForms read through the static jotform connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the form inspection requires the static jotform connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_forms_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListFormsInput,
            annotations={
                "groupId": "jotform",
                "groupLabel": "Jotform",
                "explanation": "List form IDs and titles accessible to the Jotform key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "JotformFormInspection"

    def get_steps(self) -> dex.StepList[ListFormsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
