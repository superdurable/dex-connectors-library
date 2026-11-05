# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Compare historical Python model facades with their generated wire contracts."""

import dataclasses
import importlib
import json
import sys
import types
import typing
from pathlib import Path

COMPATIBILITY_MODELS = {
    "algolia": {"SaveObjectInput"},
    "brevo": {"ContactWriteResult"},
    "cloudflare": {"CachePurge"},
    "datadog": {"QueryMetricsInput"},
    "google_gemini": {"Content", "GenerateContentRequest", "GenerateContentResponse", "Part", "Usage"},
    "google_gmail": {
        "GetMessageInput",
        "Message",
        "ReplyToMessageInput",
        "SendMessageInput",
        "SendMessageOutput",
    },
    "hackernews": {"Item"},
    "newsapi": {"SearchArticlesInput"},
    "openai": {"CreateRequest", "StructuredOutput"},
    "paypal": {"TransactionDetail", "TransactionInfo", "TransactionPage"},
    "resend": {"Email", "SendEmailInput"},
    "stripe": {
        "CheckoutSession",
        "CreateACHCheckoutSessionInput",
        "CreateCardCheckoutSessionInput",
        "CreateRefundInput",
        "GetCheckoutSessionInput",
        "GetPaymentIntentInput",
    },
    "superdurable_llm": {
        "GenerateTextRequest",
        "GenerateTextResponse",
        "textgenMessage",
        "textgenStructuredOutput",
        "textgenUsage",
    },
    "supabase": {"UpsertRowsInput"},
    "unsplash": {"Photo", "PhotoPage"},
    "vercel": {"DeploymentsPage"},
}
EXPECTED_DEVIATIONS = json.loads(
    Path(__file__).with_name("python_model_compatibility.json").read_text()
)


def wire_fields(model: type) -> dict[str, dataclasses.Field]:
    return {
        field.metadata.get("dex_json_name", field.name): field
        for field in dataclasses.fields(model)
    }


def wire_shape(annotation: object, seen: frozenset[type] = frozenset()) -> object:
    origin = typing.get_origin(annotation)
    arguments = typing.get_args(annotation)
    if origin in (typing.Union, types.UnionType):
        present = [argument for argument in arguments if argument is not type(None)]
        if len(present) == 1:
            return ("optional", wire_shape(present[0], seen))
        return (
            "union",
            tuple(sorted(repr(wire_shape(argument, seen)) for argument in present)),
        )
    if origin in (list, tuple):
        return ("sequence", wire_shape(arguments[0], seen) if arguments else None)
    if origin is dict:
        return ("map", wire_shape(arguments[0], seen), wire_shape(arguments[1], seen))
    if isinstance(annotation, type) and dataclasses.is_dataclass(annotation):
        if annotation in seen:
            return "recursive-record"
        hints = typing.get_type_hints(annotation)
        return (
            "record",
            tuple(
                sorted(
                    (
                        wire_name,
                        wire_shape(
                            hints.get(field.name, field.type), seen | {annotation}
                        ),
                    )
                    for wire_name, field in wire_fields(annotation).items()
                )
            ),
        )
    if annotation is typing.Any:
        return "any"
    return getattr(annotation, "__name__", str(annotation))


def default_shape(value: object) -> object:
    if dataclasses.is_dataclass(value) and not isinstance(value, type):
        return (
            "record",
            tuple(
                sorted(
                    (name, default_shape(getattr(value, field.name)))
                    for name, field in wire_fields(type(value)).items()
                )
            ),
        )
    if isinstance(value, (list, tuple)):
        return (type(value).__name__, tuple(default_shape(item) for item in value))
    if isinstance(value, dict):
        return tuple(sorted((key, default_shape(item)) for key, item in value.items()))
    return value


def field_default_shape(field: dataclasses.Field) -> object:
    if field.default is not dataclasses.MISSING:
        return ("default", default_shape(field.default))
    if field.default_factory is not dataclasses.MISSING:
        return ("default", default_shape(field.default_factory()))
    return ("required",)


def compare_models(package_directory: Path) -> None:
    packages = [
        path.name
        for path in (package_directory / "src" / "dex_connectors").iterdir()
        if path.is_dir() and (path / "__init__.py").exists()
    ]
    if len(packages) != 1:
        raise AssertionError(
            f"expected one Python connector package in {package_directory}"
        )
    package_name = packages[0]
    public = importlib.import_module(f"dex_connectors.{package_name}.models")
    generated = importlib.import_module(
        f"dex_connectors.{package_name}._generated.models"
    )
    shadows = {
        name
        for name in dir(generated)
        if isinstance(getattr(generated, name), type)
        and dataclasses.is_dataclass(getattr(generated, name))
        and hasattr(public, name)
        and dataclasses.is_dataclass(getattr(public, name))
        and getattr(public, name) is not getattr(generated, name)
    }
    expected = COMPATIBILITY_MODELS.get(package_name, set())
    if shadows != expected:
        raise AssertionError(
            f"{package_name} compatibility models differ: {shadows ^ expected}"
        )
    observed_deviations = set()
    for model_name in sorted(shadows):
        public_model = getattr(public, model_name)
        generated_model = getattr(generated, model_name)
        public_fields = wire_fields(public_model)
        generated_fields = wire_fields(generated_model)
        if public_fields.keys() != generated_fields.keys():
            raise AssertionError(f"{package_name}.{model_name} JSON fields differ")
        public_hints = typing.get_type_hints(public_model)
        generated_hints = typing.get_type_hints(generated_model)
        for wire_name, public_field in public_fields.items():
            generated_field = generated_fields[wire_name]
            public_metadata = {
                key: value
                for key, value in public_field.metadata.items()
                if key != "dex_json_name"
            }
            generated_metadata = {
                key: value
                for key, value in generated_field.metadata.items()
                if key != "dex_json_name"
            }
            if public_metadata != generated_metadata:
                raise AssertionError(
                    f"{package_name}.{model_name}.{wire_name} JSON metadata differs"
                )
            public_type = public_hints.get(public_field.name, public_field.type)
            generated_type = generated_hints.get(
                generated_field.name, generated_field.type
            )
            actual = {
                "publicType": repr(wire_shape(public_type)),
                "generatedType": repr(wire_shape(generated_type)),
                "publicDefault": repr(field_default_shape(public_field)),
                "generatedDefault": repr(field_default_shape(generated_field)),
            }
            key = f"{package_name}.{model_name}.{wire_name}"
            if (
                actual["publicType"] == actual["generatedType"]
                and actual["publicDefault"] == actual["generatedDefault"]
            ):
                if key in EXPECTED_DEVIATIONS:
                    raise AssertionError(
                        f"{key} no longer needs a compatibility deviation"
                    )
            elif EXPECTED_DEVIATIONS.get(key) != actual:
                raise AssertionError(f"{key} compatibility shape differs: {actual}")
            else:
                observed_deviations.add(key)
    expected_deviations = {
        key for key in EXPECTED_DEVIATIONS if key.startswith(package_name + ".")
    }
    if observed_deviations != expected_deviations:
        raise AssertionError(
            f"{package_name} compatibility deviations differ: "
            f"{observed_deviations ^ expected_deviations}"
        )


if __name__ == "__main__":
    compare_models(Path(sys.argv[1]))
