# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable summary Flow using the OpenAI createResponse Mutation with streamed text."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    MutationResult,
)

from dex_connectors.openai import (
    CREATE_RESPONSE_DEFINITION,
    UI_MODEL_PICKER_PORT_MODEL,
    UI_UNIT_MODEL_PICKER,
    Connection,
    CreateRequest,
    Response,
    Usage,
    new_create_response_step,
)

FLOW_TYPE = "OpenAISummarizeText"
CONNECTION_NAME = "openai-api"
SUMMARIZE_TEXT_STEP_TYPE = "SummarizeText"
SUMMARY_OPERATION_ID = CREATE_RESPONSE_DEFINITION.operation.operation_id
_MAX_TEXT_BYTES = 64 << 10


@dataclass(frozen=True, slots=True)
class SummaryRequest:
    """Text to summarize; at most 64 KiB."""

    text: str


@dataclass(frozen=True, slots=True)
class SummaryOutcome:
    """Selected branch and the stored response's summary, identity, and usage."""

    branch: str
    summary: str
    response_id: str = field(metadata={"dex_json_name": "responseId"})
    status: str
    usage: Usage
    model: str = field(default="", metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class SummaryModelConfiguration:
    """Model chosen in Dex Web's model picker; blank uses the connection's model."""

    model: str = ""


_REQUEST = dex.Attribute("openai-summary-request", SummaryRequest)
_OUTCOME = dex.Attribute("openai-summary-outcome", SummaryOutcome)
SUMMARY_TEXT_STREAM = dex.Stream("openai-summary-text", str, 1 << 20)


# dex:group group-id:summary group-label:"Summary"
# dex:explanation text:"Check and store the submitted text before asking OpenAI for a summary."
class _RecordSummaryRequest(dex.Step[SummaryRequest]):
    def get_step_type(self) -> str:
        return "RecordSummaryRequest"

    def execute(self, context: dex.Context, input: SummaryRequest) -> dex.StepDecision:
        if not input.text.strip() or len(input.text.encode()) > _MAX_TEXT_BYTES:
            return dex.force_fail("summary request requires non-empty text of at most 64 KiB")
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef(SUMMARIZE_TEXT_STEP_TYPE), input)


# dex:group group-id:summary group-label:"Summary"
# dex:explanation text:"Record the completed summary, its response ID, and token usage."
class _RecordSummaryOutcome(dex.Step[MutationResult[Response]]):
    def get_step_type(self) -> str:
        return "RecordSummaryOutcome"

    def execute(self, context: dex.Context, input: MutationResult[Response]) -> dex.StepDecision:
        outcome = SummaryOutcome(
            branch=input.branch,
            summary=input.value.output_text,
            response_id=input.value.id,
            status=input.value.status,
            usage=input.value.usage,
            model=input.value.model,
        )
        _OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


class SummarizeTextFlow(dex.Flow[SummaryRequest]):
    """Store an OpenAI response that summarizes text, streaming the summary while it is written."""

    def __init__(self, connection: Connection, summary_model: SummaryModelConfiguration) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("summarize text requires the static openai-api connection")
        self._summary_model = summary_model.model.strip()
        self._record_request = _RecordSummaryRequest()
        self._record_outcome = _RecordSummaryOutcome()
        self._summarize = new_create_response_step(
            step_type=SUMMARIZE_TEXT_STEP_TYPE,
            step_input_type=SummaryRequest,
            annotations={
                "groupId": "summary",
                "groupLabel": "Summary",
                "explanation": "Store an OpenAI Response that summarizes the submitted text, streaming the "
                "summary while it is written.",
            },
            configuration_ui=ConnectorConfigurationUI(
                (
                    ConnectorUIUnit(
                        "summaryModel",
                        UI_UNIT_MODEL_PICKER,
                        "Summary model",
                        "Choose the OpenAI model that writes the summary from OpenAI's live model list, "
                        "or keep the connection's model, which an empty pick uses.",
                        False,
                        (ConnectorUIBinding(UI_MODEL_PICKER_PORT_MODEL, "/model"),),
                    ),
                )
            ),
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=self.map_to_create_request,
            text_stream=SUMMARY_TEXT_STREAM,
            completed=self._record_outcome,
        )

    def map_to_create_request(self, request: SummaryRequest) -> CreateRequest:
        """Ask for a short, faithful summary with the configured model."""
        return CreateRequest(
            input=request.text,
            model=self._summary_model,
            instructions=(
                "Summarize the user's text in at most three sentences. Use only facts stated in the text."
            ),
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[SummaryRequest]:
        """Register request recording, the OpenAI create Step, and outcome recording."""
        return dex.StepList.start_step(self._record_request).other_steps(
            self._summarize, self._record_outcome
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the request, outcome, and streamed summary text."""
        return dex.PersistenceSchema.of(_REQUEST, _OUTCOME, SUMMARY_TEXT_STREAM)
