# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Compare historical Python model facades with their generated wire contracts."""

import dataclasses
import importlib
import sys
import types
import typing
from pathlib import Path

COMPATIBILITY_MODELS = {
    "algolia": {"SaveObjectInput"},
    "brevo": {"ContactWriteResult"},
    "cloudflare": {"CachePurge"},
    "datadog": {"QueryMetricsInput"},
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
    "supabase": {"UpsertRowsInput"},
    "unsplash": {"Photo", "PhotoPage"},
    "vercel": {"DeploymentsPage"},
}


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
            return wire_shape(present[0], seen)
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
            if wire_shape(public_type) != wire_shape(generated_type):
                raise AssertionError(
                    f"{package_name}.{model_name}.{wire_name} JSON type differs"
                )


if __name__ == "__main__":
    compare_models(Path(sys.argv[1]))
