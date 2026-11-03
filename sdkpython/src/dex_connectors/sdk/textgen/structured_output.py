# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Portable JSON Schema subset and exact local output validation."""

from __future__ import annotations

import copy
import json
import math
import re
from datetime import date, datetime, time
from decimal import Decimal, InvalidOperation
from typing import Any, NoReturn

from ..errors import Failure, FailureKind
from .contracts import StructuredOutput, StructuredOutputRules, TextGenerationRequest

_NAME = re.compile(r"^[A-Za-z0-9_-]{1,64}$")
_UUID = re.compile(r"^[0-9A-Fa-f]{8}(?:-[0-9A-Fa-f]{4}){3}-[0-9A-Fa-f]{12}$")
_KEYWORDS = {
    "type",
    "properties",
    "required",
    "additionalProperties",
    "items",
    "enum",
    "const",
    "description",
    "title",
    "minimum",
    "maximum",
    "minLength",
    "maxLength",
    "minItems",
    "maxItems",
    "format",
}
_TYPES = {"object", "array", "string", "number", "integer", "boolean", "null"}
_FORMATS = {"date-time", "date", "time", "uuid"}
_DATE_TIME = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:\d{2})")
_DATE = re.compile(r"\d{4}-\d{2}-\d{2}")
_TIME = re.compile(r"\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:\d{2})")


def _go_json(value: object) -> str:
    def encode(item: object) -> str:
        if item is None:
            return "null"
        if isinstance(item, bool):
            return "true" if item else "false"
        if isinstance(item, str):
            safe_text = "".join("\ufffd" if 0xD800 <= ord(char) <= 0xDFFF else char for char in item)
            encoded = json.dumps(safe_text, ensure_ascii=False)
            for char in ("<", ">", "&", "\u2028", "\u2029"):
                encoded = encoded.replace(char, f"\\u{ord(char):04x}")
            return encoded
        if isinstance(item, int):
            return str(item)
        if isinstance(item, float):
            if not math.isfinite(item):
                raise ValueError("JSON number must be finite")
            if item == 0:
                return "-0" if math.copysign(1, item) < 0 else "0"
            if 1e-6 <= abs(item) < 1e21:
                literal = format(Decimal(str(item)), "f")
                return literal.rstrip("0").rstrip(".") if "." in literal else literal
            mantissa, exponent = repr(item).split("e")
            return mantissa.removesuffix(".0") + f"e{int(exponent):+d}"
        if isinstance(item, dict):
            if any(not isinstance(key, str) for key in item):
                raise TypeError("JSON object keys must be strings")
            return "{" + ",".join(encode(key) + ":" + encode(item[key]) for key in sorted(item)) + "}"
        if isinstance(item, (list, tuple)):
            return "[" + ",".join(encode(child) for child in item) + "]"
        raise TypeError("value is not JSON serializable")

    return encode(value)


class _LocationError(ValueError):
    def __init__(self, pointer: str, reason: str) -> None:
        super().__init__(f"at {pointer or 'the root'}: {reason}")


def _at(pointer: str, reason: str) -> NoReturn:
    raise _LocationError(pointer, reason)


def _pointer(segment: str) -> str:
    return segment.replace("~", "~0").replace("/", "~1")


def _schema_type(node: dict[str, Any], pointer: str) -> tuple[str, bool]:
    value = node.get("type")
    if isinstance(value, str) and value in _TYPES:
        return value, False
    if (
        isinstance(value, list)
        and len(value) == 2
        and all(isinstance(item, str) for item in value)
        and "null" in value
        and value[0] != value[1]
    ):
        primary = value[0] if value[1] == "null" else value[1]
        if primary in _TYPES:
            return primary, True
    raise _LocationError(pointer, '"type" must be one JSON type or a pair of one type and "null"')


def _bounded_number(value: object) -> Decimal | None:
    if isinstance(value, bool) or not isinstance(value, (int, float, Decimal)):
        return None
    literal = str(value)
    if len(literal) > 256:
        return None
    if "e" in literal.lower():
        try:
            exponent = int(literal.lower().split("e", 1)[1])
        except ValueError:
            return None
        if abs(exponent) > 400:
            return None
    try:
        number = Decimal(literal)
    except InvalidOperation:
        return None
    return number if number.is_finite() else None


def _count_bound(node: dict[str, Any], pointer: str, minimum: str, maximum: str) -> None:
    values: list[int | None] = []
    for keyword in (minimum, maximum):
        value = node.get(keyword)
        if value is None:
            values.append(None)
        elif (
            isinstance(value, bool)
            or not isinstance(value, (int, float))
            or (isinstance(value, float) and (not math.isfinite(value) or not value.is_integer()))
            or not 0 <= value < 2**63
        ):
            _at(pointer, f'"{keyword}" must be a non-negative integer')
        else:
            values.append(int(value))
    if values[0] is not None and values[1] is not None and values[0] > values[1]:
        _at(pointer, f'"{minimum}" exceeds "{maximum}"')


def _check_node(node: dict[str, Any], pointer: str, depth: int) -> None:
    if depth > 10:
        _at(pointer, "the schema is nested deeper than 10 levels")
    for keyword in sorted(node):
        if keyword not in _KEYWORDS:
            shortened = keyword[:64] + ("..." if len(keyword) > 64 else "")
            _at(pointer, f'keyword "{shortened}" is outside the portable subset')
    primary, _ = _schema_type(node, pointer)
    typed = {
        "properties": "object",
        "required": "object",
        "additionalProperties": "object",
        "items": "array",
        "minItems": "array",
        "maxItems": "array",
        "minLength": "string",
        "maxLength": "string",
        "format": "string",
    }
    for keyword in sorted(node):
        if keyword in typed and typed[keyword] != primary:
            _at(pointer, f'keyword "{keyword}" applies only to type "{typed[keyword]}"')
        if keyword in {"minimum", "maximum"} and primary not in {"number", "integer"}:
            _at(pointer, f'keyword "{keyword}" applies only to number and integer types')
    for keyword in ("description", "title"):
        if keyword in node and not isinstance(node[keyword], str):
            _at(pointer, f'"{keyword}" must be a string')
    if "enum" in node:
        values = node["enum"]
        if not isinstance(values, list) or not values:
            _at(pointer, '"enum" must be a non-empty array')
        if any(not _scalar(value) for value in values):
            _at(pointer, '"enum" values must be strings, numbers, booleans, or null')
        if any(_number_like(value) and _bounded_number(value) is None for value in values):
            _at(pointer, '"enum" has a number that is out of range')
    if "const" in node:
        value = node["const"]
        if not _scalar(value):
            _at(pointer, '"const" must be a string, number, boolean, or null')
        if _number_like(value) and _bounded_number(value) is None:
            _at(pointer, '"const" is a number that is out of range')
    if primary == "object":
        properties = node.get("properties")
        if not isinstance(properties, dict):
            _at(pointer, 'an object requires a "properties" object')
        if node.get("additionalProperties") is not False:
            _at(pointer, 'an object must set "additionalProperties" to false')
        required = node.get("required")
        if not isinstance(required, list):
            _at(pointer, 'an object must list every property in "required"')
        if any(not isinstance(name, str) for name in required) or len(set(required)) != len(required):
            _at(pointer, '"required" must list property names once each')
        if any(name not in properties for name in required):
            _at(pointer, '"required" lists a property that "properties" does not declare')
        if set(required) != set(properties):
            _at(
                pointer,
                'an object must list every property in "required"; '
                'use type [T, "null"] for an optional value',
            )
        for name in sorted(properties):
            child = properties[name]
            location = pointer + "/properties/" + _pointer(name)
            if not isinstance(child, dict):
                _at(location, "a property schema must be an object")
            _check_node(child, location, depth + 1)
    elif primary == "array":
        _count_bound(node, pointer, "minItems", "maxItems")
        child = node.get("items")
        if not isinstance(child, dict):
            _at(pointer, 'an array requires an "items" schema object')
        _check_node(child, pointer + "/items", depth + 1)
    elif primary == "string":
        if "format" in node and (not isinstance(node["format"], str) or node["format"] not in _FORMATS):
            _at(pointer, '"format" must be date-time, date, time, or uuid')
        _count_bound(node, pointer, "minLength", "maxLength")
    elif primary in {"number", "integer"}:
        bounds: list[Decimal | None] = []
        for keyword in ("minimum", "maximum"):
            if keyword in node:
                number = _bounded_number(node[keyword])
                if number is None:
                    _at(pointer, f'"{keyword}" is a number that is out of range')
                bounds.append(number)
            else:
                bounds.append(None)
        if bounds[0] is not None and bounds[1] is not None and bounds[0] > bounds[1]:
            _at(pointer, '"minimum" exceeds "maximum"')


def _number_like(value: object) -> bool:
    return isinstance(value, (int, float, Decimal)) and not isinstance(value, bool)


def _scalar(value: object) -> bool:
    return value is None or isinstance(value, (str, bool)) or _number_like(value)


def canonical_schema(output: StructuredOutput) -> dict[str, Any]:
    """Validate and detach the portable schema from application-owned values."""
    if not _NAME.fullmatch(output.name):
        raise ValueError("structured output name must match ^[A-Za-z0-9_-]{1,64}$")
    if output.schema is None:
        raise ValueError("structured output schema is required")
    try:
        canonical = json.loads(_go_json(output.schema))
    except (TypeError, ValueError, OverflowError) as error:
        raise ValueError("structured output schema must be JSON serializable") from error
    if not isinstance(canonical, dict):
        raise ValueError("structured output schema must be a JSON object")
    primary, nullable = _schema_type(canonical, "")
    if primary != "object" or nullable:
        _at("", 'the root must have type "object"')
    _check_node(canonical, "", 1)
    return canonical


def validate_portable_request(request: TextGenerationRequest) -> Failure | None:
    """Return the same portable validation failure as a real Query defect."""
    _, failure = _check_portable_request(request)
    return failure


def _check_portable_request(request: TextGenerationRequest) -> tuple[dict[str, Any] | None, Failure | None]:
    """Check once and return the canonical schema retained by the attempt."""
    message = ""
    canonical: dict[str, Any] | None = None
    if not request.messages:
        message = "at least one message is required"
    else:
        for item in request.messages:
            if item.role not in ("user", "assistant"):
                message = "message role must be user or assistant"
                break
            if item.text == "":
                message = "every message requires text"
                break
    if not message:
        if request.max_output_tokens < 0:
            message = "maxOutputTokens cannot be negative"
        elif request.structured_output is not None:
            try:
                canonical = canonical_schema(request.structured_output)
            except _LocationError as error:
                message = "structured output schema is not portable " + str(error)
            except ValueError as error:
                message = str(error)
    return canonical, Failure(FailureKind.VALIDATION, "", "", message) if message else None


def transform_schema_for_provider(schema: dict[str, Any], rules: StructuredOutputRules) -> dict[str, Any]:
    """Copy and adjust only provider-facing keywords; keep local checks intact."""
    transformed = copy.deepcopy(schema)

    def visit(node: dict[str, Any]) -> None:
        constraints = []
        for keyword in sorted(set(rules.keywords_moved_to_description)):
            if keyword in node:
                constraints.append(f"{keyword}: {_go_json(node.pop(keyword))}")
        if constraints:
            node["description"] = (
                str(node.get("description", "")) + " (" + ", ".join(constraints) + ")"
            ).strip()
        for keyword in rules.keywords_removed:
            node.pop(keyword, None)
        for child in node.get("properties", {}).values():
            visit(child)
        if "items" in node:
            visit(node["items"])

    visit(transformed)
    return transformed


def json_object_instruction(output: StructuredOutput, schema: dict[str, Any]) -> str:
    """Build the JSON-only instruction used by providers without schema mode."""
    instruction = (
        "Respond with only one JSON object, without Markdown code fences, that matches the JSON Schema named "
    )
    instruction += _go_json(output.name)
    if output.description:
        instruction += f" ({output.description})"
    return instruction + ":\n" + _go_json(schema)


def extract_structured_answer(request: TextGenerationRequest, generated_text: str) -> str:
    """Strip one JSON fence and check the answer against the original schema."""
    if request.structured_output is None:
        return generated_text
    schema = canonical_schema(request.structured_output)
    return _extract_structured_answer_from_schema(schema, generated_text)


def _extract_structured_answer_from_schema(schema: dict[str, Any], generated_text: str) -> str:
    """Check one answer against the schema saved before provider dispatch."""
    text = generated_text.strip()
    if text.startswith("```") and text.endswith("```"):
        inside = text[3:-3]
        if "\n" in inside:
            info, contents = inside.split("\n", 1)
            if info.strip().lower() in ("", "json"):
                text = contents.strip()
    try:
        decoder = json.JSONDecoder(
            parse_int=Decimal,
            parse_float=Decimal,
            parse_constant=lambda _: _at("", "the text is not one JSON value"),
        )
        value, end = decoder.raw_decode(text)
        if text[end:].strip():
            _at("", "the text has data after the JSON value")
    except (json.JSONDecodeError, InvalidOperation):
        _at("", "the text is not one JSON value")
    _validate_instance(value, schema, "")
    return text


def _same_scalar(expected: object, actual: object) -> bool:
    if _number_like(expected) and _number_like(actual):
        return _bounded_number(expected) == _bounded_number(actual)
    return type(expected) is type(actual) and expected == actual


def _validate_instance(value: Any, schema: dict[str, Any], pointer: str) -> None:
    primary, nullable = _schema_type(schema, pointer)
    if value is None:
        if not nullable and primary != "null":
            _at(pointer, f"expected type {primary}, found null")
    else:
        number = _bounded_number(value) if _number_like(value) else None
        if _number_like(value) and number is None:
            _at(pointer, "the number is out of range")
        matches = {
            "object": isinstance(value, dict),
            "array": isinstance(value, list),
            "string": isinstance(value, str),
            "boolean": isinstance(value, bool),
            "number": number is not None,
            "integer": number is not None and number == number.to_integral_value(),
            "null": False,
        }
        if not matches[primary]:
            _at(pointer, "expected type " + primary)
    if "enum" in schema and not any(_same_scalar(item, value) for item in schema["enum"]):
        _at(pointer, "the value is not one of the enum values")
    if "const" in schema and not _same_scalar(schema["const"], value):
        _at(pointer, "the value does not equal const")
    if value is None:
        return
    if primary == "object":
        for name in sorted(value):
            if name not in schema["properties"]:
                _at(pointer, "the object has a property the schema does not declare")
        for name, child in sorted(schema["properties"].items()):
            location = pointer + "/" + _pointer(name)
            if name not in value:
                _at(location, "a required property is missing")
            _validate_instance(value[name], child, location)
    elif primary == "array":
        _validate_count(len(value), schema, pointer, "minItems", "maxItems", "items")
        for index, item in enumerate(value):
            _validate_instance(item, schema["items"], pointer + f"/{index}")
    elif primary == "string":
        _validate_count(len(value), schema, pointer, "minLength", "maxLength", "characters")
        if "format" in schema and not _matches_format(value, schema["format"]):
            _at(pointer, "the string does not match format " + schema["format"])
    elif primary in ("number", "integer"):
        for keyword, reason in (
            ("minimum", "the value is below minimum"),
            ("maximum", "the value is above maximum"),
        ):
            if keyword in schema:
                bound = _bounded_number(schema[keyword])
                if (
                    bound is not None
                    and number is not None
                    and (
                        (keyword == "minimum" and number < bound) or (keyword == "maximum" and number > bound)
                    )
                ):
                    _at(pointer, reason)


def _validate_count(
    count: int, schema: dict[str, Any], pointer: str, minimum: str, maximum: str, unit: str
) -> None:
    if minimum in schema and count < schema[minimum]:
        _at(pointer, f"the value has fewer {unit} than {minimum}")
    if maximum in schema and count > schema[maximum]:
        _at(pointer, f"the value has more {unit} than {maximum}")


def _matches_format(value: str, format_name: str) -> bool:
    try:
        if format_name == "uuid":
            return bool(_UUID.fullmatch(value))
        if format_name == "date-time":
            return bool(_DATE_TIME.fullmatch(value)) and bool(
                datetime.fromisoformat(value.replace(",", ".").replace("Z", "+00:00"))
            )
        if format_name == "date":
            return bool(_DATE.fullmatch(value)) and bool(date.fromisoformat(value))
        if format_name == "time":
            return bool(_TIME.fullmatch(value)) and bool(
                time.fromisoformat(value.replace(",", ".").replace("Z", "+00:00"))
            )
    except ValueError:
        return False
    return False
