# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Decode Responses API objects exactly as the Go connector's wire structs accept them."""

from typing import Any

from .models import Response, Usage


def decode_wire_response(document: Any) -> Response:
    """Convert a Responses API object, joining output_text parts like the Go connector."""
    if not isinstance(document, dict):
        raise ValueError
    texts: list[str] = []
    for output in _list(document, "output"):
        if not isinstance(output, dict):
            raise ValueError
        _string(output, "type")
        for content in _list(output, "content"):
            if not isinstance(content, dict):
                raise ValueError
            content_type = _string(content, "type")
            text = _string(content, "text")
            _string(content, "refusal")
            if content_type == "output_text":
                texts.append(text)
    incomplete = document.get("incomplete_details")
    if incomplete is not None:
        if not isinstance(incomplete, dict):
            raise ValueError
        _string(incomplete, "reason")
    usage = _object(document, "usage")
    input_details = _object(usage, "input_tokens_details")
    output_details = _object(usage, "output_tokens_details")
    return Response(
        _string(document, "id"),
        _string(document, "model"),
        _string(document, "status"),
        "".join(texts),
        Usage(
            _integer(usage, "input_tokens"),
            _integer(input_details, "cached_tokens"),
            _integer(usage, "output_tokens"),
            _integer(output_details, "reasoning_tokens"),
            _integer(usage, "total_tokens"),
        ),
    )


def is_safe_response_id(response_id: str) -> bool:
    """Accept 1 to 128 ASCII letters, digits, underscores, and hyphens."""
    if not 1 <= len(response_id) <= 128:
        return False
    return all(
        character.isascii() and (character.isalnum() or character in "_-") for character in response_id
    )


def _string(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value


def _integer(document: dict[str, Any], name: str) -> int:
    value = document.get(name)
    if value is None:
        return 0
    if type(value) is not int:
        raise ValueError
    return value


def _list(document: dict[str, Any], name: str) -> list[Any]:
    value = document.get(name)
    if value is None:
        return []
    if not isinstance(value, list):
        raise ValueError
    return value


def _object(document: dict[str, Any], name: str) -> dict[str, Any]:
    value = document.get(name)
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise ValueError
    return value
