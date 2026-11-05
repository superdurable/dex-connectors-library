# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Keep unsupported raw JSON numbers out of durable text generation inputs."""

from decimal import Decimal

import dex
import pytest

from dex_connectors.superdurable_llm import GenerateTextRequest, Message, StructuredOutput


def test_decimal_schema_number_fails_before_dex_persistence() -> None:
    """The SDK codec cannot preserve Decimal precision in a Flow request."""
    request = GenerateTextRequest(
        messages=(Message("user", "Answer with a number."),),
        structured_output=StructuredOutput(
            "answer",
            {
                "type": "object",
                "properties": {"value": {"type": "number", "const": Decimal("0.123456789012345678901")}},
            },
        ),
    )
    with pytest.raises(TypeError, match="Decimal"):
        dex.CodecRegistry().resolve(GenerateTextRequest).encode(request)
