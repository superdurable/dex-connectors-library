# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check OpenAI mock validation against the real connector's local contract."""

import pytest

from dex_connectors.openai import CreateRequest, Response, StructuredOutput
from dex_connectors.openai.mock_checks import (
    validate_create_response_answer,
    validate_create_response_request,
)


def test_request_check_rejects_missing_input_and_nonserializable_schema() -> None:
    assert validate_create_response_request(CreateRequest()) is not None
    assert (
        validate_create_response_request(
            CreateRequest(input="hi", structured_output=StructuredOutput("reply", {"value": object()}))
        )
        is not None
    )
    assert validate_create_response_request(CreateRequest(input="hi")) is None


def test_completed_strict_answer_checks_json_schema() -> None:
    request = CreateRequest(
        input="hi",
        structured_output=StructuredOutput(
            "reply",
            {
                "type": "object",
                "properties": {"text": {"type": "string"}},
                "required": ["text"],
                "additionalProperties": False,
            },
            strict=True,
        ),
    )
    validate_create_response_answer(request, Response(status="completed", output_text='{"text":"ok"}'))
    with pytest.raises(ValueError):
        validate_create_response_answer(request, Response(status="completed", output_text='{"text":5}'))
    with pytest.raises(ValueError):
        validate_create_response_answer(request, Response(status="completed", output_text='{"text":NaN}'))
    with pytest.raises(ValueError):
        validate_create_response_answer(request, Response(status="incomplete", output_text='{"text":"ok"}'))
