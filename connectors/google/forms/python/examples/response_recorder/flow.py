# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read a form, list one response page, and read one response by ID."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.google_forms import (
    Connection,
    Form,
    FormResponse,
    GetFormInput,
    GetResponseInput,
    ListResponsesInput,
    ResponsePage,
    new_get_form_step,
    new_get_response_step,
    new_list_responses_step,
)

CONNECTION_NAME = "google-forms-intake"
_FORM_ID = dex.Attribute("google-forms-python-form-id", str)


@dataclass(frozen=True, slots=True)
class RecorderInput:
    """Form whose first response page should be read."""

    form_id: str


@dataclass(frozen=True, slots=True)
class RecorderOutcome:
    """One form and its first response, if present."""

    form: Form
    page: ResponsePage
    response: FormResponse | None = None


# dex:group group-id:google-forms group-label:"Google Forms"
# dex:explanation text:"Store the form ID and read its questions before recording responses."
class PrepareRequest(dex.Step[RecorderInput]):
    """Retain the form identity across provider Steps."""

    def get_step_type(self) -> str:
        return "PrepareFormsRequest"

    def execute(self, context: dex.Context, input: RecorderInput) -> dex.StepDecision:
        if not input.form_id:
            return dex.force_fail("form ID is required")
        _FORM_ID.set(context, input.form_id)
        return dex.go_to(dex.StepRef("ReadGoogleForm"), input)


# dex:group group-id:google-forms group-label:"Google Forms"
# dex:explanation text:"Carry the form result into the response listing Step."
class FormRead(dex.Step[QueryResult[Form]]):
    """Start the response list after the form is found."""

    def get_step_type(self) -> str:
        return "GoogleFormRead"

    def execute(self, context: dex.Context, input: QueryResult[Form]) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListGoogleResponses"), GetFormInput(input.value.form_id))


# dex:group group-id:google-forms group-label:"Google Forms"
# dex:explanation text:"Complete an empty page or read its first response by ID."
class PageRead(dex.Step[QueryResult[ResponsePage]]):
    """Read the first response when Google returned one."""

    def get_step_type(self) -> str:
        return "GoogleResponsePageRead"

    def execute(self, context: dex.Context, input: QueryResult[ResponsePage]) -> dex.StepDecision:
        if not input.value.responses:
            return dex.graceful_complete(input.value)
        return dex.go_to(
            dex.StepRef("ReadGoogleResponse"),
            GetResponseInput(input.value.form_id, input.value.responses[0].response_id),
        )


# dex:group group-id:google-forms group-label:"Google Forms"
# dex:explanation text:"Complete after the individual Google response is found."
class ResponseRead(dex.Step[QueryResult[FormResponse]]):
    """Complete with the provider response record."""

    def get_step_type(self) -> str:
        return "GoogleResponseRead"

    def execute(self, context: dex.Context, input: QueryResult[FormResponse]) -> dex.StepDecision:
        return dex.graceful_complete(input.value)


class ResponseRecorderFlow(dex.Flow[RecorderInput]):
    """Exercise all three Google Forms operations in a real Dex Flow."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the Google Forms connection name must be static")
        self._prepare = PrepareRequest()
        self._form_read = FormRead()
        self._page_read = PageRead()
        self._response_read = ResponseRead()
        self._get_form = new_get_form_step(
            step_type="ReadGoogleForm",
            step_input_type=RecorderInput,
            annotations={
                "groupId": "google-forms",
                "groupLabel": "Google Forms",
                "explanation": "Read the form's questions.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: GetFormInput(value.form_id),
            found=self._form_read,
        )
        self._list_responses = new_list_responses_step(
            step_type="ListGoogleResponses",
            step_input_type=GetFormInput,
            annotations={
                "groupId": "google-forms",
                "groupLabel": "Google Forms",
                "explanation": "List one bounded page of responses.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: ListResponsesInput(value.form_id, page_size=25),
            listed=self._page_read,
        )
        self._get_response = new_get_response_step(
            step_type="ReadGoogleResponse",
            step_input_type=GetResponseInput,
            annotations={
                "groupId": "google-forms",
                "groupLabel": "Google Forms",
                "explanation": "Read one response by its provider ID.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            found=self._response_read,
        )

    def get_flow_type(self) -> str:
        """Return the stable example Flow identity."""
        return "GoogleFormsPythonResponseRecorder"

    def get_steps(self) -> dex.StepList[RecorderInput]:
        """Register admission, three provider reads, and their success routes."""
        return dex.StepList.start_step(self._prepare).other_steps(
            self._get_form,
            self._form_read,
            self._list_responses,
            self._page_read,
            self._get_response,
            self._response_read,
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the form ID used by downstream Steps."""
        return dex.PersistenceSchema.of(_FORM_ID)
