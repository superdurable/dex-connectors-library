# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Summarize text through the connection's provider-neutral generateText Query."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.superdurable_llm import (
    Connection,
    GenerateTextRequest,
    GenerateTextResponse,
    Message,
    new_generate_text_step,
)

CONNECTION_NAME = "llm-summarizer"
FLOW_TYPE = "LLMSummarizeTextPython"
_SUMMARY = dex.Attribute("llm-python-summary", GenerateTextResponse)


@dataclass(frozen=True, slots=True)
class SummarizeTextInput:
    """Text to summarize through the named LLM connection."""

    text: str


def _to_generate_request(input: SummarizeTextInput) -> GenerateTextRequest:
    if not input.text.strip():
        raise ValueError("text is required")
    return GenerateTextRequest(messages=(Message("user", "Summarize this text: " + input.text),))


# dex:group group-id:llm-summary group-label:"LLM summary"
# dex:explanation text:"Persist the generated summary and complete the Flow."
class _CompleteSummary(dex.Step[QueryResult[GenerateTextResponse]]):
    def get_step_type(self) -> str:
        return "CompleteLLMSummary"

    def execute(self, context: dex.Context, input: QueryResult[GenerateTextResponse]) -> dex.StepDecision:
        _SUMMARY.set(context, input.value)
        return dex.graceful_complete(input.value)


class SummarizeTextFlow(dex.Flow[SummarizeTextInput]):
    """Run one text generation Query and retain its completed answer."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("summary Flow requires the static llm connection")
        self._complete = _CompleteSummary()
        self._generate = new_generate_text_step(
            step_type="GenerateLLMSummary",
            step_input_type=SummarizeTextInput,
            annotations={
                "groupId": "llm-summary",
                "groupLabel": "LLM summary",
                "explanation": "Generate a concise summary with the connection's model.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=_to_generate_request,
            generated=self._complete,
        )

    def get_flow_type(self) -> str:
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[SummarizeTextInput]:
        return dex.StepList.start_step(self._generate).other_steps(self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        return dex.PersistenceSchema.of(_SUMMARY)
