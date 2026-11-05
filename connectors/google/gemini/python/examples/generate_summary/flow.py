# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Flow that asks Gemini for a structured JSON summary through generateContent."""

import json
from dataclasses import dataclass, field, replace
from typing import Any

import dex
from dex_connectors.sdk import ConnectorConfigurationUI, ConnectorUIBinding, ConnectorUIUnit, QueryResult

from dex_connectors.google_gemini import (
    GENERATE_CONTENT_DEFINITION,
    UI_MODEL_PICKER_PORT_MODEL,
    UI_UNIT_MODEL_PICKER,
    Connection,
    Content,
    GenerateContentRequest,
    GenerateContentResponse,
    Part,
    Usage,
    new_generate_content_step,
)

FLOW_TYPE = "GeminiGenerateSummary"
CONNECTION_NAME = "gemini-api"
GENERATE_SUMMARY_STEP_TYPE = "GenerateSummary"
SUMMARY_OPERATION_ID = GENERATE_CONTENT_DEFINITION.operation.operation_id
STATUS_GENERATED = "generated"
_MAX_TITLE_CHARACTERS = 200
_MAX_TEXT_BYTES = 64 << 10
_MAX_SUMMARY_OUTPUT_TOKENS = 4096


@dataclass(frozen=True, slots=True)
class SummaryRequest:
    """Title of at most 200 characters and text of at most 64 KiB."""

    title: str = ""
    text: str = ""


@dataclass(frozen=True, slots=True)
class Summary:
    """The structured summary Gemini returns: headline, two-sentence summary, and up to five key points."""

    headline: str = ""
    summary: str = ""
    key_points: tuple[str, ...] = field(default=(), metadata={"dex_json_name": "keyPoints"})


@dataclass(frozen=True, slots=True)
class SummaryOutcome:
    """Status, title, the parsed summary, and Gemini's finish reason, model version, and usage."""

    status: str = ""
    title: str = ""
    summary: Summary | None = field(default=None, metadata={"dex_json_omit_default": True})
    finish_reason: str = field(
        default="", metadata={"dex_json_name": "finishReason", "dex_json_omit_default": True}
    )
    block_reason: str = field(
        default="", metadata={"dex_json_name": "blockReason", "dex_json_omit_default": True}
    )
    model_version: str = field(
        default="", metadata={"dex_json_name": "modelVersion", "dex_json_omit_default": True}
    )
    usage: Usage = field(default_factory=Usage)


@dataclass(frozen=True, slots=True)
class SummaryModelConfiguration:
    """Model chosen in Dex Web's model picker; blank uses the connection's model."""

    model: str = ""


_REQUEST = dex.Attribute("gemini-summary-request", SummaryRequest)
_OUTCOME = dex.Attribute("gemini-summary-outcome", SummaryOutcome)


def summary_schema() -> dict[str, Any]:
    """Return the JSON Schema Gemini must follow for the summary."""
    return {
        "type": "object",
        "properties": {
            "headline": {"type": "string", "description": "A headline of at most twelve words."},
            "summary": {"type": "string", "description": "A two-sentence summary."},
            "keyPoints": {"type": "array", "items": {"type": "string"}, "maxItems": 5},
        },
        "required": ["headline", "summary", "keyPoints"],
        "additionalProperties": False,
    }


def parse_summary(text: str) -> Summary | None:
    """Parse Gemini's JSON text, or return None when it does not match the summary schema."""
    try:
        document = json.loads(text)
    except ValueError:
        return None
    if not isinstance(document, dict):
        return None
    headline = document.get("headline") or ""
    summary = document.get("summary") or ""
    key_points = document.get("keyPoints") or []
    if (
        not isinstance(headline, str)
        or not isinstance(summary, str)
        or not isinstance(key_points, list)
        or not all(isinstance(point, str) for point in key_points)
        or not headline.strip()
        or not summary.strip()
    ):
        return None
    return Summary(headline, summary, tuple(key_points))


# dex:group group-id:summary group-label:"Summary"
# dex:explanation text:"Validate the submitted text and persist the request before calling Gemini."
class _RecordSummaryRequest(dex.Step[SummaryRequest]):
    def get_step_type(self) -> str:
        return "RecordSummaryRequest"

    def execute(self, context: dex.Context, input: SummaryRequest) -> dex.StepDecision:
        request = replace(input, title=input.title.strip())
        if (
            not request.text.strip()
            or len(request.text.encode()) > _MAX_TEXT_BYTES
            or len(request.title) > _MAX_TITLE_CHARACTERS
        ):
            return dex.force_fail(
                "summary request requires text of at most 64 KiB and a title of at most 200 characters"
            )
        _REQUEST.set(context, request)
        return dex.go_to(dex.StepRef(GENERATE_SUMMARY_STEP_TYPE), request)


# dex:group group-id:summary group-label:"Summary"
# dex:explanation text:"Decode the structured Gemini summary and complete the Flow."
class _SummaryGenerated(dex.Step[QueryResult[GenerateContentResponse]]):
    def get_step_type(self) -> str:
        return "SummaryGenerated"

    def execute(self, context: dex.Context, input: QueryResult[GenerateContentResponse]) -> dex.StepDecision:
        request = _REQUEST.get(context)
        summary = parse_summary(input.value.text)
        if summary is None:
            return dex.force_fail("Gemini returned text that does not match the summary schema")
        outcome = SummaryOutcome(
            status=STATUS_GENERATED,
            title=request.title,
            summary=summary,
            finish_reason=input.value.finish_reason,
            model_version=input.value.model_version,
            usage=input.value.usage,
        )
        _OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


class GenerateSummaryFlow(dex.Flow[SummaryRequest]):
    """Ask Gemini for a structured JSON summary through the static gemini-api connection."""

    def __init__(self, connection: Connection, summary_model: SummaryModelConfiguration) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("generate summary requires the static gemini-api connection")
        self._summary_model = summary_model.model.strip()
        self._record_request = _RecordSummaryRequest()
        self._summary_generated = _SummaryGenerated()
        self._generate = new_generate_content_step(
            step_type=GENERATE_SUMMARY_STEP_TYPE,
            step_input_type=SummaryRequest,
            annotations={
                "groupId": "gemini",
                "groupLabel": "Gemini",
                "explanation": "Ask Gemini for a structured JSON summary of the submitted text.",
            },
            configuration_ui=ConnectorConfigurationUI(
                (
                    ConnectorUIUnit(
                        "summaryModel",
                        UI_UNIT_MODEL_PICKER,
                        "Summary model",
                        "Choose the Gemini model that writes the summary, or keep the connection's model.",
                        False,
                        (ConnectorUIBinding(UI_MODEL_PICKER_PORT_MODEL, "/model"),),
                    ),
                )
            ),
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=self.map_to_generate_content_request,
            generated=self._summary_generated,
        )

    def map_to_generate_content_request(self, request: SummaryRequest) -> GenerateContentRequest:
        """Ask for a faithful structured summary with the configured model."""
        return GenerateContentRequest(
            model=self._summary_model,
            system_instruction=(
                "You write concise newsletter summaries. Use only facts stated in the provided text. "
                "Return a headline, a two-sentence summary, and up to five key points."
            ),
            contents=(Content("user", (Part(f"Title: {request.title}\n\n{request.text}"),)),),
            response_json_schema=summary_schema(),
            max_output_tokens=_MAX_SUMMARY_OUTPUT_TOKENS,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[SummaryRequest]:
        """Register request recording, the Gemini generation, and outcome recording."""
        return dex.StepList.start_step(self._record_request).other_steps(
            self._generate, self._summary_generated
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the request and outcome."""
        return dex.PersistenceSchema.of(_REQUEST, _OUTCOME)
