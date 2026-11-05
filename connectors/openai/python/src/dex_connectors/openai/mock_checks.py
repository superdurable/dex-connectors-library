# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Match the Go connector's createResponse mock request and answer checks."""

import json

from dex_connectors.sdk import Failure, FailureKind, textgen

from .client import (
    CONNECTOR_ID,
    INVALID_CREATE_INPUT_MESSAGE,
    NOT_SERIALIZABLE_MESSAGE,
    is_printable_model_id,
)
from .models import CreateRequest, Response


def _reject_json_constant(value: str) -> None:
    raise ValueError("non-JSON number: " + value)


def validate_create_response_request(request: CreateRequest) -> Failure | None:
    """Reject only the local conditions that the real create operation rejects."""
    model = request.model.strip() or "gpt-6-sol"
    if request.input is None or not is_printable_model_id(model):
        return Failure(FailureKind.VALIDATION, CONNECTOR_ID, "createResponse", INVALID_CREATE_INPUT_MESSAGE)
    try:
        json.dumps(request.input, allow_nan=False)
        if request.structured_output is not None:
            json.dumps(request.structured_output.schema, allow_nan=False)
    except (TypeError, ValueError):
        return Failure(FailureKind.VALIDATION, CONNECTOR_ID, "createResponse", NOT_SERIALIZABLE_MESSAGE)
    return None


def validate_create_response_answer(request: CreateRequest, answer: Response) -> None:
    """Require completed status and valid structured JSON when the request asks for it."""
    if answer.status and answer.status != "completed":
        raise ValueError("a completed answer must have status completed or none")
    output = request.structured_output
    if output is None or not answer.output_text:
        return
    try:
        json.loads(answer.output_text, parse_constant=_reject_json_constant)
    except ValueError:
        raise ValueError("completed structured answer must be exactly one JSON value") from None
    if not output.strict:
        return
    portable = textgen.TextGenerationRequest(
        messages=(textgen.Message(textgen.MessageRole.USER, "check"),),
        structured_output=textgen.StructuredOutput(output.name, output.schema, output.description),
    )
    if textgen.validate_portable_request(portable) is not None:
        return
    try:
        textgen.extract_structured_answer(portable, answer.output_text)
    except ValueError as error:
        raise ValueError("completed structured answer does not match its schema") from error
