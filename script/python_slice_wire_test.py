# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Decode actual Go connector JSON slices with supported Dex SDK releases."""

import dataclasses
import importlib
import json
import os
import subprocess
import sys
import tempfile
import typing
from pathlib import Path

# package: (Go/Python model, wire field, populated Go JSON value, expected Python value, hashable)
CASES = {
    "cohere": [
        ("EmbedInput", "texts", ["one", "two"], ("one", "two"), True),
        (
            "EmbedOutput",
            "embeddings",
            [[1.25, 2.5], None, []],
            ((1.25, 2.5), (), ()),
            True,
        ),
        ("RerankInput", "documents", ["first", "second"], ("first", "second"), True),
    ],
    "gitlab": [
        ("Issue", "labels", ["bug", "urgent"], ("bug", "urgent"), True),
        ("UpdateIssueLabelsInput", "labels", ["bug"], ("bug",), True),
    ],
    "hackernews": [("StoryIDs", "ids", [11, 22], (11, 22), True)],
    "cloudflare": [
        (
            "PurgeCacheByURLsInput",
            "urls",
            ["https://example.test/a"],
            ("https://example.test/a",),
            True,
        )
    ],
    "datadog": [
        (
            "MetricSeries",
            "pointlist",
            [[1, 0.5], None, [2, None]],
            ((1, 0.5), (), (2, None)),
            True,
        )
    ],
    "resend": [
        ("SendEmailInput", "to", ["a@example.test"], ("a@example.test",), True),
        ("Email", "to", ["a@example.test"], ("a@example.test",), True),
    ],
    "supabase": [
        ("UpsertRowsInput", "rows", [{"id": 1}, None], ({"id": 1}, None), False)
    ],
    "smartsheet": [("SheetRow", "cells", ["text", 1, None], ("text", 1, None), True)],
    "google_gmail": [
        ("SendMessageInput", "to", ["a@example.test"], ("a@example.test",), True),
        (
            "SendMessageOutput",
            "recipients",
            ["a@example.test"],
            ("a@example.test",),
            True,
        ),
    ],
    "pinecone": [
        ("Vector", "values", [1.25, 2.5], (1.25, 2.5), False),
        ("QueryVectorsInput", "vector", [1.25, 2.5], (1.25, 2.5), True),
    ],
}


def assert_tuple_tree(actual: object, expected: object) -> None:
    if isinstance(expected, tuple):
        assert isinstance(actual, tuple), (actual, expected)
        assert len(actual) == len(expected)
        for actual_item, expected_item in zip(actual, expected):
            assert_tuple_tree(actual_item, expected_item)
    else:
        assert actual == expected


def decode_go_output(package_name: str) -> None:
    import dex

    models = importlib.import_module(f"dex_connectors.{package_name}._generated.models")
    documents = json.load(sys.stdin)
    for model_name, field_name, _, expected, should_hash in CASES[package_name]:
        model = getattr(models, model_name)
        field = next(
            field for field in dataclasses.fields(model) if field.name == field_name
        )
        assert typing.get_type_hints(model)[field_name] == field.type
        codec = dex.CodecRegistry().resolve(model)
        for case_name, expected_value in (("populated", expected), ("nil", ())):
            name = f"{model_name}.{field_name}.{case_name}"
            decoded = codec.decode(dex.Value(dex.WireKind.JSON, documents[name]))
            actual_value = getattr(decoded, field_name)
            assert_tuple_tree(actual_value, expected_value)
            assert decoded == dataclasses.replace(
                decoded, **{field_name: expected_value}
            )
            if should_hash:
                hash(decoded)


def go_json_documents(go_directory: Path, cases: list[tuple]) -> dict[str, str]:
    module_name = next(
        line.split()[1]
        for line in (go_directory / "go.mod").read_text().splitlines()
        if line.startswith("module ")
    )
    statements = []
    for model_name, field_name, populated, _, _ in cases:
        for case_name, field_value in (("populated", populated), ("nil", None)):
            key = f"{model_name}.{field_name}.{case_name}"
            source_json = json.dumps({field_name: field_value})
            statements.extend(
                [
                    "{",
                    f"var model connector.{model_name}",
                    f"if err := json.Unmarshal([]byte({json.dumps(source_json)}), &model); err != nil {{ panic(err) }}",
                    "payload, err := json.Marshal(model)",
                    "if err != nil { panic(err) }",
                    f"documents[{json.dumps(key)}] = string(payload)",
                    "}",
                ]
            )
    source = "\n".join(
        [
            "// Copyright (c) 2026 Super Durable",
            "// SPDX-License-Identifier: MIT",
            "package main",
            f'import ("encoding/json"; "os"; connector {json.dumps(module_name)})',
            "func main() {",
            "documents := map[string]string{}",
            *statements,
            "result, err := json.Marshal(documents)",
            "if err != nil { panic(err) }",
            "if _, err := os.Stdout.Write(result); err != nil { panic(err) }",
            "}",
        ]
    )
    with tempfile.TemporaryDirectory() as temporary_directory:
        source_path = Path(temporary_directory) / "main.go"
        source_path.write_text(source)
        result = subprocess.run(
            ["go", "run", str(source_path)],
            cwd=go_directory,
            env={**os.environ, "GOWORK": "off"},
            capture_output=True,
            text=True,
            check=True,
        )
    return json.loads(result.stdout)


def check_supported_sdks() -> None:
    repository = Path(__file__).resolve().parent.parent
    for package_name, cases in CASES.items():
        manifest = next(
            path
            for path in (repository / "connectors").rglob("connector.yaml")
            if (
                path.parent / "python" / "src" / "dex_connectors" / package_name
            ).is_dir()
        )
        documents = go_json_documents(manifest.parent / "go", cases)
        for sdk_version in ("1.28.0", "1.30.0"):
            subprocess.run(
                [
                    "uv",
                    "run",
                    "--project",
                    str(manifest.parent / "python"),
                    "--with",
                    f"dex-python-sdk=={sdk_version}",
                    "python",
                    str(Path(__file__).resolve()),
                    "--decode",
                    package_name,
                ],
                cwd=repository,
                input=json.dumps(documents),
                text=True,
                check=True,
            )
    print(
        f"{sum(map(len, CASES.values()))} Go slice fields decode on Dex SDK 1.28.0 and 1.30.0"
    )


if __name__ == "__main__":
    if len(sys.argv) == 3 and sys.argv[1] == "--decode":
        decode_go_output(sys.argv[2])
    else:
        check_supported_sdks()
