# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed OpenAI Responses requests, stored responses, and token usage."""

from dataclasses import dataclass, field
from typing import Any

from ._generated.models import Response as Response
from ._generated.models import RetrieveRequest as RetrieveRequest
from ._generated.models import Usage as Usage


@dataclass(frozen=True, slots=True)
class StructuredOutput:
    """JSON Schema that constrains the response text; strict asks OpenAI to enforce it exactly."""

    name: str
    schema: dict[str, Any]
    description: str = field(default="", metadata={"dex_json_omit_default": True})
    strict: bool = False


@dataclass(frozen=True, slots=True)
class CreateRequest:
    """One Responses API create request.

    input is a string or a JSON-serializable list of Responses input items. A
    blank or omitted model uses the connection's model.
    """

    input: Any = field(default=None, metadata={"dex_json_name": "Input"})
    model: str = field(default="", metadata={"dex_json_name": "Model", "dex_json_omit_default": True})
    instructions: str = field(default="", metadata={"dex_json_name": "Instructions"})
    structured_output: StructuredOutput | None = field(
        default=None, metadata={"dex_json_name": "StructuredOutput"}
    )


RetrieveRequest.__doc__ = "Provider response ID returned by createResponse, such as resp_..."
Usage.__doc__ = "Token counts reported by OpenAI for one response."
Response.__doc__ = "Stored OpenAI response; output_text joins every output_text content part."
