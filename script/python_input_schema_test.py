# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check published input schemas against the real Python models and Dex codec."""

import dataclasses
import importlib
import json
import sys
from pathlib import Path
from typing import Any

from dex import CodecRegistry, Value, WireKind

MAXIMUM_SAMPLE_DEPTH = 4
# gomodel lists constants only for enums in the connector package, so this SDK enum is a plain string.
CLOSED_SDK_ENUM_SAMPLES = {"role": "user"}


def sample_value(schema: dict[str, Any], root: dict[str, Any], depth: int) -> object:
    """Build a non-default value, so a field that omits its default still encodes."""
    if "$ref" in schema:
        return sample_value(root["$defs"][schema["$ref"].rsplit("/", 1)[-1]], root, depth)
    if "anyOf" in schema:
        return sample_value(schema["anyOf"][0], root, depth)
    if "enum" in schema:
        return next(value for value in schema["enum"] if value not in (None, "", 0))
    kind = schema.get("type")
    if isinstance(kind, list):
        kind = kind[0]
    if kind == "string":
        if schema.get("format") == "date-time":
            return "2026-01-02T03:04:05Z"
        return "AQID" if "contentEncoding" in schema else "sample"
    if kind == "integer":
        return 7
    if kind == "number":
        return 1.5
    if kind == "boolean":
        return True
    if kind == "array":
        return [sample_value(schema["items"], root, depth + 1)]
    if kind == "object" and "properties" in schema:
        required = set(schema.get("required", ()))
        return {
            name: CLOSED_SDK_ENUM_SAMPLES.get(name) or sample_value(property_schema, root, depth + 1)
            for name, property_schema in schema["properties"].items()
            if name in required or depth < MAXIMUM_SAMPLE_DEPTH
        }
    if kind == "object":
        return {"key": sample_value(schema["additionalProperties"], root, depth + 1)}
    return {"opaque": "value"}


def check_operation(models: object, factory: dict[str, Any]) -> None:
    label = factory["operationId"]
    model = getattr(models, factory["inputType"])
    if not dataclasses.is_dataclass(model):
        raise AssertionError(f"{label}: {factory['inputType']} is not a public dataclass")
    schema = factory["inputSchema"]
    fields = {field.metadata.get("dex_json_name", field.name): field for field in dataclasses.fields(model)}
    if fields.keys() != schema["properties"].keys():
        raise AssertionError(f"{label}: JSON names differ: {fields.keys() ^ schema['properties'].keys()}")
    for name, field in fields.items():
        has_default = field.default is not dataclasses.MISSING or field.default_factory is not dataclasses.MISSING
        if not has_default and name not in schema.get("required", ()):
            raise AssertionError(f"{label}: {name} has no Python default but the schema does not require it")
        if field.metadata.get("dex_json_omit_default") and name in schema.get("required", ()):
            raise AssertionError(f"{label}: {name} omits its default but the schema requires it")
    sample = sample_value(schema, schema, 0)
    codec = CodecRegistry().resolve(model)
    decoded = codec.decode(Value(WireKind.JSON, json.dumps(sample)))
    encoded = json.loads(codec.encode(decoded).data)
    if not sample.keys() <= encoded.keys():
        raise AssertionError(f"{label}: the Dex codec dropped {sample.keys() - encoded.keys()}")


def main() -> None:
    package_directory = Path(sys.argv[1])
    manifests = list((package_directory / "src" / "dex_connectors").glob("*/dex-connector.json"))
    if len(manifests) != 1:
        raise AssertionError(f"expected one Connector manifest in {package_directory}")
    models = importlib.import_module(f"dex_connectors.{manifests[0].parent.name}.models")
    for factory in json.loads(manifests[0].read_text())["factories"]:
        check_operation(models, factory)


if __name__ == "__main__":
    main()
