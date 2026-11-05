# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Render Python connector contracts from the manifest and referenced Go structs."""

import json
import keyword
import re
import sys

payload = json.load(sys.stdin)
manifest = payload["manifest"]
metadata = manifest["metadata"]
spec = manifest["spec"]
auth_methods = spec["auth"].get("methods", [])


def auth_fields():
    if not auth_methods:
        return spec["auth"].get("fields", [])
    fields = {}
    for method in auth_methods:
        for field in method["fields"]:
            fields.setdefault(field["name"], field)
    return list(fields.values())


def configuration_fields():
    fields = list(spec["configuration"]["fields"])
    for method in auth_methods:
        for field in (method.get("configuration") or {}).get("fields", []):
            fields.append({**field, "required": False})
    return fields


operations_by_name = {operation["name"]: operation for operation in spec["operations"]}
operations = [
    operations_by_name[name] for name in spec["languages"]["python"]["operations"]
]
connector_id = metadata["name"]
connection_provider = "PROVIDER" if spec["provider"] != connector_id else "CONNECTOR_ID"
connection_definitions = "CONNECTOR_ID, PROVIDER" if connection_provider == "PROVIDER" else "CONNECTOR_ID"
package_name = payload["packageName"]
mock_package_name = payload["mockPackageName"]
model_aliases = payload.get("modelAliases") or {}


models = [
    {
        "name": model["Name"],
        "description": model.get("Comment", ""),
        "fields": [
            {
                "jsonName": field_info["Name"],
                "omitEmpty": field_info["Optional"],
                "hasPresence": field_info.get("HasPresence", False),
                "description": field_info.get("Comment", ""),
                "irType": field_info["Type"],
            }
            for field_info in model["Fields"]
        ],
    }
    for model in payload["models"] or []
]


def snake(value):
    value = re.sub(r"(URLs|IDs)$", lambda match: match.group(1).title(), value)
    value = re.sub(r"([A-Z]+)([A-Z][a-z])", r"\1_\2", value)
    value = re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", value)
    return value.replace("-", "_").lower()


def python_field_name(value):
    name = snake(value)
    return name + "_" if keyword.iskeyword(name) else name


def constant(value):
    return snake(value).upper()


def quoted(value):
    return json.dumps(value, ensure_ascii=False)


def duration(value):
    if not value:
        return "timedelta(0)"
    parts = re.findall(r"(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)", value)
    if not parts or "".join(number + unit for number, unit in parts) != value:
        raise ValueError(f"unsupported duration {value!r}")
    seconds = sum(
        float(number)
        * {"ns": 1e-9, "us": 1e-6, "µs": 1e-6, "ms": 1e-3, "s": 1, "m": 60, "h": 3600}[
            unit
        ]
        for number, unit in parts
    )
    return f"timedelta(seconds={seconds:g})"


def referenced_models(type_info):
    kind = type_info["Kind"]
    if kind == "ref":
        return {type_info["Ref"]}
    if kind == "list":
        return referenced_models(type_info["Elem"])
    if kind == "map":
        return referenced_models(type_info["Key"]) | referenced_models(
            type_info["Elem"]
        )
    return set()


def python_type(
    type_info, defined_models, required_models, optional=False, has_presence=False
):
    kind = type_info["Kind"]
    if kind == "ref":
        name = type_info["Ref"]
        annotation = name if name in defined_models else quoted(name)
        if name in required_models:
            default = None
        elif name in defined_models:
            default = f"_dataclass_field(default_factory={name})"
        else:
            default = f"_dataclass_field(default_factory=lambda: {name}())"
    elif kind == "list":
        element, _ = python_type(type_info["Elem"], defined_models, required_models)
        annotation, default = f"tuple[{element}, ...]", "()"
    elif kind == "map":
        key, _ = python_type(type_info["Key"], defined_models, required_models)
        element, _ = python_type(type_info["Elem"], defined_models, required_models)
        annotation, default = (
            f"dict[{key}, {element}]",
            "_dataclass_field(default_factory=dict)",
        )
    elif kind == "enum":
        annotation, default = (
            ("str", None) if type_info["EnumBase"] == "string" else ("int", None)
        )
    else:
        annotation, default = {
            "string": ("str", "''"),
            "bool": ("bool", "False"),
            "int": ("int", "0"),
            "int64": ("int", "0"),
            "float": ("float", "0.0"),
            "time": (
                "datetime",
                "_dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC))",
            ),
            "bytes": ("str", "''"),
            "unknown": ("Any", "None"),
        }[kind]
    if (
        (type_info["Nullable"] and kind != "list")
        or has_presence
        or (optional and kind in ("enum", "ref"))
    ):
        if kind != "unknown":
            if kind == "ref" and type_info["Ref"] not in defined_models:
                annotation = f"Optional[{annotation}]"
            else:
                annotation += " | None"
        default = "None"
    return annotation, default


def codec_list_type(type_info, defined_models, required_models):
    if type_info["Kind"] != "list":
        return python_type(type_info, defined_models, required_models)[0]
    element = codec_list_type(type_info["Elem"], defined_models, required_models)
    annotation = f"tuple[{element}, ...]"
    return annotation + " | None" if type_info["Nullable"] else annotation


def list_depth(type_info):
    return (
        1 + list_depth(type_info["Elem"]) if type_info["Elem"]["Kind"] == "list" else 1
    )


class ModelDependencyOrder:
    def __init__(self):
        self.by_name = {model["name"]: model for model in models}
        self.visited = set()
        self.ordered = []

    def visit(self, model):
        if model["name"] in self.visited:
            return
        self.visited.add(model["name"])
        for field_info in model["fields"]:
            for dependency in referenced_models(field_info["irType"]):
                if dependency in self.by_name:
                    self.visit(self.by_name[dependency])
        self.ordered.append(model)


def render_models():
    required_models = set()
    while True:
        newly_required = {
            model["name"]
            for model in models
            for field_info in model["fields"]
            if not field_info["omitEmpty"]
            and not field_info["irType"]["Nullable"]
            and (
                field_info["irType"]["Kind"] == "enum"
                or (
                    field_info["irType"]["Kind"] == "ref"
                    and field_info["irType"]["Ref"] in required_models
                )
            )
        }
        if newly_required <= required_models:
            break
        required_models |= newly_required
    dependency_order = ModelDependencyOrder()
    for model in models:
        dependency_order.visit(model)
    lines = []
    if any(
        field_info["irType"]["Kind"] == "list"
        and field_info["irType"]["Nullable"]
        and not field_info["hasPresence"]
        for model in models
        for field_info in model["fields"]
    ):
        lines.extend(
            [
                "def _replace_nil_slices(value: Any, depth: int) -> Any:",
                "    if value is None:",
                "        return ()",
                "    if depth == 1:",
                "        return tuple(value)",
                "    return tuple(_replace_nil_slices(item, depth - 1) for item in value)",
                "",
                "",
            ]
        )
    defined_models = set()
    for model in dependency_order.ordered:
        description = model.get("description", "").strip()
        if description:
            lines.extend("# " + line for line in description.splitlines())
        lines += ["@dataclass(frozen=True, slots=True)", f"class {model['name']}:"]
        if not model["fields"]:
            lines.append("    pass")
        has_default_field = False
        nullable_list_fields = []
        for field_info in model["fields"]:
            python_name = python_field_name(field_info["jsonName"])
            field_type, default = python_type(
                field_info["irType"],
                defined_models,
                required_models,
                field_info["omitEmpty"],
                field_info["hasPresence"],
            )
            wire_name = field_info["jsonName"]
            metadata = {}
            if wire_name != python_name:
                metadata["dex_json_name"] = wire_name
            if field_info["omitEmpty"]:
                metadata["dex_json_omit_default"] = True
            if field_info["irType"]["Kind"] == "time":
                metadata["dex_json_format"] = "rfc3339"
            if field_info.get("description"):
                for comment_line in field_info["description"].strip().splitlines():
                    lines.append("    # " + comment_line)
            if default is None and has_default_field:
                arguments = "kw_only=True"
                if metadata:
                    arguments += f", metadata={metadata!r}"
                lines.append(
                    f"    {python_name}: {field_type} = _dataclass_field({arguments})"
                )
            elif default is None and not metadata:
                lines.append(f"    {python_name}: {field_type}")
            elif default is None:
                lines.append(
                    f"    {python_name}: {field_type} = _dataclass_field(metadata={metadata!r})"
                )
            elif metadata:
                if default.startswith("_dataclass_field("):
                    merged = default[:-1] + f", metadata={metadata!r})"
                    lines.append(f"    {python_name}: {field_type} = {merged}")
                else:
                    lines.append(
                        f"    {python_name}: {field_type} = _dataclass_field(default={default}, metadata={metadata!r})"
                    )
            else:
                lines.append(f"    {python_name}: {field_type} = {default}")
            if default is not None:
                has_default_field = True
            if (
                field_info["irType"]["Kind"] == "list"
                and field_info["irType"]["Nullable"]
                and not field_info["hasPresence"]
            ):
                nullable_list_fields.append((python_name, field_info["irType"]))
        if nullable_list_fields:
            lines.extend(["", "    def __post_init__(self) -> None:"])
            for python_name, type_info in nullable_list_fields:
                lines.append(
                    f"        object.__setattr__(self, {python_name!r}, "
                    f"_replace_nil_slices(self.{python_name}, {list_depth(type_info)}))"
                )
        lines += ["", ""]
        for python_name, type_info in nullable_list_fields:
            codec_annotation = codec_list_type(
                type_info, defined_models, required_models
            )
            lines.append(
                f"{model['name']}.__annotations__[{python_name!r}] = {codec_annotation}"
            )
            lines.append(
                f"{model['name']}.__dataclass_fields__[{python_name!r}].type = {codec_annotation}"
            )
        lines.append(
            f"setattr({model['name']}, '__module__', 'dex_connectors.{package_name}.models')"
        )
        lines += ["", ""]
        defined_models.add(model["name"])
    body = "\n".join(lines).rstrip() + "\n"
    header = [
        "# Copyright (c) 2026 Super Durable",
        "# SPDX-License-Identifier: MIT",
        "",
        "from dataclasses import dataclass"
        + (", field as _dataclass_field" if "_dataclass_field(" in body else ""),
    ]
    if "datetime" in body:
        header.append("from datetime import UTC, datetime")
    typing_imports = [name for name in ("Any", "Literal", "Optional") if name in body]
    if typing_imports:
        header.append("from typing import " + ", ".join(typing_imports))
    if "SecretString" in body:
        header += ["", "from dex_connectors.sdk import SecretString"]
    return "\n".join(header) + "\n\n" + body


def render_definitions():
    lines = [
        "# Copyright (c) 2026 Super Durable",
        "# SPDX-License-Identifier: MIT",
        "",
        "from datetime import timedelta",
        "",
        "from dex_connectors.sdk import (",
        "    BranchDefinition,",
        "    MutationDefinition,",
        "    OperationRef,",
        "    QueryDefinition,",
        "    RetryPolicy,",
        "    StepDefaults,",
        "    default_connector_step_execute_retry_policy,",
        ")",
        "",
        f"CONNECTOR_ID = {quoted(connector_id)}",
        f"PROVIDER = {quoted(spec['provider'])}",
    ]
    branches = {}
    for operation in operations:
        lines.append(
            f"{constant(operation['name'])}_OPERATION_ID = {quoted(operation['name'])}"
        )
        for branch in operation["branches"]:
            branches[branch["id"]] = branch["id"]
    for branch in sorted(branches):
        lines.append(f"{constant(branch)} = {quoted(branch)}")
    lines.append("")
    for operation in operations:
        definition = (
            "MutationDefinition"
            if operation["kind"] == "mutation"
            else "QueryDefinition"
        )
        lines += [
            f"{constant(operation['name'])}_DEFINITION = {definition}(",
            f"    OperationRef(CONNECTOR_ID, {constant(operation['name'])}_OPERATION_ID),",
            "    (",
        ]
        for branch in operation["branches"]:
            lines.append(
                f"        BranchDefinition({constant(branch['id'])}, {quoted(branch['description'])}, {bool(branch.get('optional', False))}),"
            )
        lines += ["    ),"]
        execution = operation["execution"]
        retry = execution.get("retry")
        retry_expr = "default_connector_step_execute_retry_policy()"
        if retry:
            retry_expr = (
                "RetryPolicy("
                + ", ".join(
                    [
                        duration(retry["initialInterval"]),
                        str(retry["backoffCoefficient"]),
                        duration(retry["maximumInterval"]),
                        str(retry["maximumAttempts"]),
                        duration(retry["totalDuration"]),
                    ]
                )
                + ")"
            )
        durability = {"sync": 1, "async": 2}[execution["durability"]]
        lines.append("    StepDefaults(")
        lines.append(f"        {duration(execution['executeMethodTimeout'])},")
        lines.append(f"        {durability},")
        if execution.get("heartbeatTimeout"):
            lines.append(
                f"        heartbeat_timeout={duration(execution['heartbeatTimeout'])},"
            )
        lines.append(f"        execute_retry={retry_expr},")
        lines += ["    ),", ")", ""]
    return "\n".join(lines).rstrip() + "\n"


def render_config():
    lines = [
        "# Copyright (c) 2026 Super Durable",
        "# SPDX-License-Identifier: MIT",
        "",
        "from dataclasses import dataclass",
    ]
    credential_fields = auth_fields()
    if any(field["type"] == "secretString" for field in credential_fields):
        lines += ["", "from dex_connectors.sdk import SecretString"]
    lines += [
        "",
        "",
        "@dataclass(frozen=True, slots=True)",
        "class Config:",
        "",
    ]
    config_fields = configuration_fields()
    if not config_fields:
        lines.append("    pass")
    rendered_config_fields = []
    for field_info in config_fields:
        name = snake(field_info["name"])
        if field_info["type"] == "duration":
            name += "_seconds"
            field_type = "float"
            default = (
                duration(field_info["default"])
                .replace("timedelta(seconds=", "")
                .rstrip(")")
                if "default" in field_info
                else None
            )
        elif field_info["type"] in ("integer", "number"):
            field_type = "int" if field_info["type"] == "integer" else "float"
            default = str(field_info["default"]) if "default" in field_info else None
        elif field_info["type"] == "boolean":
            field_type = "bool"
            default = str(field_info["default"]) if "default" in field_info else None
        elif field_info["type"] == "stringList":
            field_type = "tuple[str, ...]"
            default = (
                repr(tuple(field_info["default"])) if "default" in field_info else None
            )
        elif field_info["type"] in ("string", "url", "enum"):
            field_type = "str"
            default = quoted(field_info["default"]) if "default" in field_info else None
        else:
            raise ValueError(
                f"unsupported Python configuration type {field_info['type']!r}"
            )
        if auth_methods and default is None and not field_info["required"]:
            default = {
                "string": "''", "url": "''", "enum": "''", "stringList": "()",
                "integer": "0", "number": "0.0", "boolean": "False", "duration": "0.0",
            }[field_info["type"]]
        rendered_config_fields.append((name, field_type, default))
    for name, field_type, default in sorted(
        rendered_config_fields, key=lambda field: field[2] is not None
    ):
        lines.append(
            f"    {name}: {field_type}"
            + (f" = {default}" if default is not None else "")
        )
    lines += [
        "",
        f"setattr(Config, '__module__', 'dex_connectors.{package_name}.client')",
        "",
    ]
    if credential_fields:
        lines += [
            "",
            "@dataclass(frozen=True, slots=True)",
            "class Credentials:",
            "",
        ]
        if auth_methods:
            lines.append(f"    auth_method_id: str = {quoted(spec['auth']['defaultMethod'])}")
        for field_info in credential_fields:
            field_type = {"secretString": "SecretString", "integer": "int", "duration": "int", "boolean": "bool", "stringList": "list[str]", "stringMap": "dict[str, str]"}.get(field_info["type"], "str")
            if auth_methods:
                if not field_info["required"] and field_info["type"] == "secretString":
                    lines.append(f"    {snake(field_info['name'])}: {field_type} = SecretString('')")
                elif not field_info["required"] and field_info["type"] in ("string", "enum", "url"):
                    lines.append(f"    {snake(field_info['name'])}: {field_type} = ''")
                else:
                    lines.append(f"    {snake(field_info['name'])}: {field_type} | None = None")
            else:
                lines.append(f"    {snake(field_info['name'])}: {field_type}")
        lines += [
            "",
            f"setattr(Credentials, '__module__', 'dex_connectors.{package_name}.client')",
            "",
        ]
    return "\n".join(lines).rstrip() + "\n"


def render_steps():
    has_query = any(operation["kind"] == "query" for operation in operations)
    has_mutation = any(operation["kind"] == "mutation" for operation in operations)
    lines = [
        "# Copyright (c) 2026 Super Durable",
        "# SPDX-License-Identifier: MIT",
        "",
        "from collections.abc import Callable, Mapping",
        "from typing import Any, TypeVar",
        "",
        "import dex",
        "from dex_connectors.sdk import (",
    ]
    if has_mutation:
        lines += ["    MutationResult,", "    MutationStep,", "    MutationStepConfig,"]
    if has_query:
        lines += ["    QueryResult,", "    QueryStep,", "    QueryStepConfig,"]
    lines += [
        "    StepAnnotations,",
        ")",
        "",
        "from ..connection import Connection",
        "from ..models import "
        + ", ".join(
            sorted(
                {
                    name
                    for op in operations
                    for name in (op["inputType"], op["outputType"])
                }
            )
        ),
        "",
        'StepInput = TypeVar("StepInput")',
        "",
    ]
    for operation in operations:
        kind = "Mutation" if operation["kind"] == "mutation" else "Query"
        result = kind + "Result"
        input_type = operation["inputType"]
        output_type = operation["outputType"]
        name = snake(operation["name"])
        lines += [
            f"def new_{name}_step(",
            "    *,",
            "    step_type: str,",
            "    step_input_type: type[StepInput],",
            "    annotations: StepAnnotations | Mapping[str, str],",
            "    connection: Connection,",
            "    connection_name: str,",
            f"    map_to_operation_input: Callable[[StepInput], {input_type}],",
        ]
        for branch in operation["branches"]:
            branch_name = snake(branch["id"])
            optional = branch.get("optional", False)
            annotation = f"dex.StepTarget[{result}[{output_type}]]"
            lines.append(
                f"    {branch_name}: {annotation}"
                + (" | None = None," if optional else ",")
            )
        lines.append(
            f"    result_attribute: dex.Attribute[{result}[{output_type}]] | None = None,"
        )
        lines += [
            "    step_options_override: dex.StepOptions | None = None,",
            f") -> {kind}Step[StepInput, {input_type}, {output_type}]:",
        ]
        lines += [
            "    if connection is None or connection.reference.name != connection_name:",
            f"        raise ValueError({quoted(metadata['displayName'] + ' Step connection name must match its connection reference')})",
        ]
        lines += [
            "    if isinstance(annotations, StepAnnotations):",
            "        step_annotations = annotations",
            "    else:",
            '        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:',
            f"            raise ValueError({quoted(metadata['displayName'] + ' Step annotations require groupId, groupLabel, and explanation')})",
            "        if any(not isinstance(value, str) for value in annotations.values()):",
            f"            raise ValueError({quoted(metadata['displayName'] + ' Step annotations must be strings')})",
            '        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])',
        ]
        lines.append("    branches = {")
        for branch in operation["branches"]:
            if not branch.get("optional", False):
                branch_name = snake(branch["id"])
                lines.append(f"        {quoted(branch['id'])}: {branch_name},")
        lines.append("    }")
        for branch in operation["branches"]:
            if branch.get("optional", False):
                branch_name = snake(branch["id"])
                lines += [
                    f"    if {branch_name} is not None:",
                    f"        branches[{quoted(branch['id'])}] = {branch_name}",
                ]
        lines += [
            f"    return {kind}Step(",
            f"        {kind}StepConfig(",
            "            step_type=step_type,",
            "            step_input_type=step_input_type,",
            "            annotations=step_annotations,",
            f"            operation=_bound_operation(connection, {quoted(name)}),",
            "            connection=connection.reference,",
            "            map_to_operation_input=map_to_operation_input,",
            "            branches=branches,",
        ]
        lines.append("            result_attribute=result_attribute,")
        lines += [
            "            step_options_override=step_options_override,",
            "        )",
            "    )",
            "",
            f"setattr(new_{name}_step, '__module__', 'dex_connectors.{package_name}.steps')",
            "",
        ]
    lines += [
        "",
        "def _bound_operation(connection: Connection, operation_name: str) -> Any:",
        "    source = getattr(connection, operation_name, None)",
        "    if source is None:",
        '        source = getattr(getattr(connection, "operations", None), operation_name, None)',
        "    if source is None:",
        '        raise ValueError(f"connection operation {operation_name} is missing")',
        "    return source() if callable(source) else source",
    ]
    return "\n".join(lines).rstrip() + "\n"


def render_connection():
    by_name = {snake(operation["name"]): operation for operation in operations}
    order = [snake(operation["name"]) for operation in operations]
    kinds = {operation["kind"] for operation in operations}
    imports = ["ConnectionRef"]
    if "mutation" in kinds:
        imports.append("Mutation")
    if "query" in kinds:
        imports.append("Query")
    model_names = sorted(
        {
            name
            for operation in operations
            for name in (operation["inputType"], operation["outputType"])
        }
    )
    definition_names = sorted(
        {constant(operation["name"]) + "_DEFINITION" for operation in operations}
    )
    connection_label = metadata["displayName"]
    has_refresh = payload["hasDeclaredTokenGrants"]
    lines = [
        "# Copyright (c) 2026 Super Durable",
        "# SPDX-License-Identifier: MIT",
        "",
        "from typing import Any",
        *(["from re import search", "from urllib.parse import urlsplit"] if any(field_info["type"] == "url" for field_info in auth_fields()) else []),
        "",
        "import httpx",
        "from dex_connectors.sdk import "
        + ", ".join(imports + (["Call", "SecretString"] if auth_fields() else [])
                    + (["CredentialRefreshDriver"] if has_refresh else [])),
        "from dex_connectors.sdk.projectconfig import "
        + (
            "ConnectionKey, LoadedProject"
            if auth_fields()
            else "LoadedProject"
        ),
        *(["from dex_connectors.sdk.projectconfig.provider import RefreshingCredentialProvider",
            "from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver"] if has_refresh else []),
        "",
        "from ..client import Client, Config"
        + (", Credentials" if auth_fields() else ""),
        "from .definitions import " + connection_definitions + ", " + ", ".join(definition_names),
        *(["from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS"] if has_refresh else []),
        "from ..models import " + ", ".join(model_names),
        "",
        *([
            "def _is_absolute_credential_url(value: str) -> bool:",
            "    if value != value.strip() or any(ord(character) < 32 or ord(character) == 127 for character in value) or search(r'%(?![0-9a-fA-F]{2})', value):",
            "        return False",
            "    try:",
            "        parsed = urlsplit(value)",
            "        return bool(parsed.scheme and parsed.hostname and not any(character.isspace() for character in parsed.netloc))",
            "    except ValueError:",
            "        return False",
            "",
        ] if any(field_info["type"] == "url" for field_info in auth_fields()) else []),
        "class Connection:",
        "",
        "    __slots__ = ("
        + ", ".join(repr("_" + name) for name in order)
        + ", '_client', 'reference')",
        "",
        "    def __init__(self,",
        "        reference: ConnectionRef,",
    ]
    for name in order:
        operation = by_name[name]
        kind = "Mutation" if operation["kind"] == "mutation" else "Query"
        parameter = name
        lines.append(
            f"        {parameter}: {kind}[{operation['inputType']}, {operation['outputType']}],"
        )
    lines += [
        "        client: Client | None = None,",
        "    ) -> None:",
        "        reference.validate()",
    ]
    checks = [f"reference.provider != {connection_provider}"] + [
        name + " is None" for name in order
    ]
    lines.append("        if " + " or ".join(checks) + ":")
    noun = "operations"
    lines.append(
        f"            raise ValueError({quoted(connection_label + ' connection provider and ' + noun + ' are required')})"
    )
    for name in order:
        parameter = name
        lines.append(
            f"        if {parameter}.definition().operation != {constant(by_name[name]['name'])}_DEFINITION.operation:"
        )
        lines.append(
            f"            raise ValueError({quoted(connection_label + ' connection operation identity differs')})"
        )
    lines.append("        self.reference = reference")
    for name in order:
        lines.append(f"        self._{name} = {name}")
    lines += ["        self._client = client", ""]
    for name in order:
        operation = by_name[name]
        kind = "Mutation" if operation["kind"] == "mutation" else "Query"
        lines += [
            f"    def {name}(self) -> {kind}[{operation['inputType']}, {operation['outputType']}]:",
            f"        return self._{name}",
            "",
        ]
    lines += [
        "    async def aclose(self) -> None:",
        "        if self._client is not None:",
        "            await self._client.aclose()",
        "",
        "    def __repr__(self) -> str:",
        f"        return {quoted(package_name + '.Connection([REDACTED])')}",
        "",
        "    def __reduce__(self) -> tuple[object, ...]:",
        f"        raise TypeError({quoted(connection_label + ' connections cannot be serialized')})",
        "",
        f"setattr(Connection, '__module__', 'dex_connectors.{package_name}.connection')",
        "",
        "",
        "def new_connection(client: Client, reference: ConnectionRef) -> Connection:",
        "    if client is None:",
        f"        raise ValueError({quoted(connection_label + ' client is required')})",
        "    return Connection(reference, "
        + ", ".join(f"client.{name}()" for name in order)
        + ", client)",
        "",
        f"setattr(new_connection, '__module__', 'dex_connectors.{package_name}.connection')",
        "",
        "",
    ]
    lines.append("def new_connection_with_operations(")
    for name in order:
        operation = by_name[name]
        kind = "Mutation" if operation["kind"] == "mutation" else "Query"
        parameter = name
        lines.append(
            f"    {parameter}: {kind}[{operation['inputType']}, {operation['outputType']}],"
        )
    lines += [
        "    reference: ConnectionRef,",
        ") -> Connection:",
        "    return Connection(reference, " + ", ".join(name for name in order) + ")",
        "",
        f"setattr(new_connection_with_operations, '__module__', 'dex_connectors.{package_name}.connection')",
        "",
    ]
    credential_fields = auth_fields()
    if credential_fields:
        decoder_start = len(lines)
        lines += [
            "",
            "class _ProjectCredentialSource:",
            "",
            "    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:",
            "        self.project = project",
            "        self.key = key",
        ]
        decoder_body_start = len(lines) + 3
        lines += [
            "",
            "    async def resolve(self, call: Call) -> Credentials:",
            "        material = await self.project.resolve_credential(self.key, str(call.id))",
            "        if not isinstance(material, dict):",
            f"            raise ValueError({quoted(connection_label + ' stored credential does not match the connector credential fields')})",
        ]
        allowed = [field_info["name"] for field_info in credential_fields]
        if auth_methods:
            allowed.append("auth_method")
        needs_shared_validation = any(
            (field_info["name"] in payload["sharedRequiredFields"][method["id"]]) != field_info["required"]
            for method in (auth_methods or [{"id": "", "fields": credential_fields}])
            for field_info in method["fields"]
        )
        lines += [
            f"        if set(material) - set({tuple(allowed)!r}):",
            f"            raise ValueError({quoted(connection_label + ' stored credential does not match the connector credential fields')})",
        ]
        if needs_shared_validation:
            lines.append("        is_shared = bool(self.project.shared_connections and self.key in self.project.shared_connections)")
        for field_info in credential_fields:
            name = snake(field_info["name"])
            field_type = field_info["type"]
            check = f"isinstance({name}, str)" if field_type in ("string", "secretString", "enum", "url") else (
                f"type({name}) is int" if field_type in ("integer", "duration") else
                f"type({name}) is bool" if field_type == "boolean" else
                f"isinstance({name}, list) and all(item is None or isinstance(item, str) for item in {name})" if field_type == "stringList" else
                f"isinstance({name}, dict) and all(isinstance(key, str) and (item is None or isinstance(item, str)) for key, item in {name}.items())"
            )
            lines += [
                f"        {name} = material.get({quoted(field_info['name'])})",
                f"        if {name} is not None and not ({check}):",
                f"            raise ValueError({quoted(connection_label + ' credential ' + field_info['name'] + ' has an invalid type')})",
            ]
        if auth_methods:
            lines += [
                "        selected_method = material.get('auth_method')",
                "        if selected_method is None:",
                "            selected_method = ''",
                "        if not isinstance(selected_method, str):",
                f"            raise ValueError({quoted(connection_label + ' credential auth_method is invalid')})",
                "        method = selected_method or "
                + quoted(spec["auth"]["defaultMethod"]),
                "        values: dict[str, Any] = {'auth_method_id': method}",
            ]
            for index, method in enumerate(auth_methods):
                lines.append(("        if" if index == 0 else "        elif") + f" method == {quoted(method['id'])}:")
                for field_info in method["fields"]:
                    name = snake(field_info["name"])
                    is_shared_required = field_info["name"] in payload["sharedRequiredFields"][method["id"]]
                    if field_info["required"] or is_shared_required:
                        requirement = f"({name} is None or {name} <= 0)" if field_info["type"] in ("integer", "duration") else f"not {name}"
                        if field_info["required"] != is_shared_required:
                            requirement += " and " + ("is_shared" if is_shared_required else "not is_shared")
                        lines += [
                            f"            if {requirement}:",
                            f"                raise ValueError({quoted(connection_label + ' credential ' + field_info['name'] + (' must be positive' if field_info['type'] in ('integer', 'duration') else ' is required'))})",
                        ]
                    if field_info["type"] in ("integer", "duration"):
                        lines += [f"            if {name} is not None and {name} < 0:", f"                raise ValueError({quoted(connection_label + ' credential ' + field_info['name'] + ' cannot be negative')})"]
                    if field_info["type"] == "enum":
                        lines += [f"            if {name} and {name} not in {tuple(field_info['enum'])!r}:", f"                raise ValueError({quoted(connection_label + ' credential ' + field_info['name'] + ' is invalid')})"]
                    if field_info["type"] == "url":
                        lines += [f"            if {name} and not _is_absolute_credential_url({name}):", f"                raise ValueError({quoted(connection_label + ' credential ' + field_info['name'] + ' must be an absolute URL')})"]
                    expression = f"SecretString({name})" if field_info["type"] == "secretString" else (
                        f"[item or '' for item in {name}]" if field_info["type"] == "stringList" else
                        f"{{key: item or '' for key, item in {name}.items()}}" if field_info["type"] == "stringMap" else name
                    )
                    if not field_info["required"] and field_info["type"] == "secretString":
                        expression = f"SecretString({name} or '')"
                        lines.append(f"            values[{quoted(name)}] = {expression}")
                        continue
                    if not field_info["required"] and field_info["type"] in ("string", "enum", "url"):
                        lines.append(f"            values[{quoted(name)}] = {name} or ''")
                        continue
                    lines.append(f"            if {name}:")
                    lines.append(f"                values[{quoted(name)}] = {expression}")
            lines += [
                "        else:",
                f"            raise ValueError({quoted(connection_label + ' credential auth_method is invalid')})",
                "        return Credentials(**values)", "",
            ]
        else:
            for field_info in credential_fields:
                name = snake(field_info["name"])
                is_shared_required = field_info["name"] in payload["sharedRequiredFields"][""]
                if field_info["required"] or is_shared_required:
                    requirement = f"({name} is None or {name} <= 0)" if field_info["type"] in ("integer", "duration") else f"not {name}"
                    if field_info["required"] != is_shared_required:
                        requirement += " and " + ("is_shared" if is_shared_required else "not is_shared")
                    lines += [f"        if {requirement}:", f"            raise ValueError({quoted(connection_label + ' credential ' + field_info['name'] + (' must be positive' if field_info['type'] in ('integer', 'duration') else ' is required'))})"]
                if field_info["type"] in ("integer", "duration"):
                    lines += [f"        if {name} is not None and {name} < 0:", f"            raise ValueError({quoted(connection_label + ' credential ' + field_info['name'] + ' cannot be negative')})"]
                if field_info["type"] == "enum":
                    lines += [f"        if {name} and {name} not in {tuple(field_info['enum'])!r}:", f"            raise ValueError({quoted(connection_label + ' credential ' + field_info['name'] + ' is invalid')})"]
                if field_info["type"] == "url":
                    lines += [f"        if {name} and not _is_absolute_credential_url({name}):", f"            raise ValueError({quoted(connection_label + ' credential ' + field_info['name'] + ' must be an absolute URL')})"]
            values = []
            for field_info in credential_fields:
                name = snake(field_info["name"])
                can_be_absent = not field_info["required"] or field_info["name"] not in payload["sharedRequiredFields"][""]
                if field_info["type"] == "secretString":
                    values.append(f"SecretString({name} or '')" if can_be_absent else f"SecretString({name})")
                elif field_info["type"] in ("string", "enum", "url"):
                    values.append(f"({name} or '')" if can_be_absent else name)
                elif field_info["type"] in ("integer", "duration"):
                    values.append(f"({name} or 0)" if can_be_absent else name)
                elif field_info["type"] == "boolean":
                    values.append(f"({name} or False)" if can_be_absent else name)
                elif field_info["type"] == "stringList":
                    values.append(f"[item or '' for item in ({name} or [])]")
                else:
                    values.append(f"{{key: item or '' for key, item in ({name} or {{}}).items()}}")
            lines += ["        return Credentials(" + ", ".join(values) + ")", ""]
        if has_refresh:
            decoder = lines[decoder_body_start:]
            decoder = [line[4:] if line.startswith("    ") else line for line in decoder]
            decoder = [
                "    is_shared = shared" if "is_shared = bool(self.project.shared_connections" in line else line
                for line in decoder
            ]
            lines[decoder_start:] = [
                "",
                "def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:",
                *decoder,
                "",
                "def decode_shared_credentials(material: dict[str, Any]) -> Credentials:",
                "    return decode_credentials(material, shared=True)",
                "",
                "",
                "def encode_credentials(credentials: Credentials) -> dict[str, Any]:",
                "    material: dict[str, Any] = {}",
            ]
            if auth_methods:
                lines += [
                    "    method = credentials.auth_method_id or " + quoted(spec["auth"]["defaultMethod"]),
                    "    material['auth_method'] = method",
                ]
                for index, method in enumerate(auth_methods):
                    lines.append(("    if" if index == 0 else "    elif") + f" method == {quoted(method['id'])}:")
                    for field_info in method["fields"]:
                        name = snake(field_info["name"])
                        expression = (
                            f"(credentials.{name}.reveal() if credentials.{name} is not None else '')"
                            if field_info["type"] == "secretString" else f"credentials.{name}"
                        )
                        lines += [f"        if {expression}:", f"            material[{quoted(field_info['name'])}] = {expression}"]
                lines += [
                    "    else:",
                    f"        raise ValueError({quoted(connection_label + ' credential auth_method is invalid')})",
                ]
            else:
                for field_info in credential_fields:
                    name = snake(field_info["name"])
                    expression = f"credentials.{name}.reveal()" if field_info["type"] == "secretString" else f"credentials.{name}"
                    lines += [f"    if {expression}:", f"        material[{quoted(field_info['name'])}] = {expression}"]
            lines += [
                "    decode_credentials(material)",
                "    return material",
                "",
                "",
                "class _ProjectCredentialSource:",
                "    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:",
                "        self.provider = RefreshingCredentialProvider(",
                "            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials",
                "        )",
                f"        self.driver = ManifestCredentialRefreshDriver[Credentials]({quoted(connection_label)}, AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)",
                "",
                "    async def resolve(self, call: Call) -> Credentials:",
                "        return await self.provider.resolve_with_refresh(call, self.driver)",
                "",
                "    async def resolve_with_refresh(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:",
                "        return await self.provider.resolve_with_refresh(call, driver or self.driver)",
                "",
                "    async def resolve_after_rejection(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:",
                "        return await self.provider.resolve_after_rejection(call, driver or self.driver)",
                "",
            ]
    lines += [
        "",
        "def new_project_connection(",
        "    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None",
        ") -> Connection:",
        "    if project is None:",
        f"        raise ValueError({quoted(connection_label + ' connection requires loaded project configuration')})",
        f"    reference = ConnectionRef({connection_provider}, connection_name)",
        "    reference.validate()",
    ]
    if credential_fields:
        lines.append("    key = ConnectionKey(CONNECTOR_ID, connection_name)")
    lines += [
        "    matching = [",
        "        entry",
        "        for entry in project.configuration.connections",
        "        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name",
        "    ]",
        "    if len(matching) != 1:",
        f"        raise ValueError({quoted(connection_label + ' project connection is missing or duplicated')})",
        f"    if matching[0].provider != {connection_provider}:",
        f"        raise ValueError({quoted(connection_label + ' project connection provider differs')})",
        "    config = _decode_config(matching[0].configuration)",
    ]
    client_args = (
        ("config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client"
         if has_refresh else "config, _ProjectCredentialSource(project, key), http_client")
        if credential_fields
        else "config, http_client"
    )
    if has_refresh:
        lines.append("    http_client = http_client or httpx.AsyncClient()")
    lines += [
        f"    client = Client({client_args})",
        "    return new_connection(client, reference)",
        "",
        f"setattr(new_project_connection, '__module__', 'dex_connectors.{package_name}.connection')",
        "",
        "",
        "def _decode_config(value: dict[str, Any]) -> Config:",
    ]
    config_fields = configuration_fields()
    allowed = (
        "{"
        + ", ".join(quoted(field_info["name"]) for field_info in config_fields)
        + "}"
    )
    lines += [
        f"    if set(value) - {allowed}:",
        f"        raise ValueError({quoted(connection_label + ' configuration has unknown fields')})",
    ]
    lines.append("    config_arguments: dict[str, Any] = {}")
    for field_info in config_fields:
        name = snake(field_info["name"])
        if field_info["type"] == "duration":
            name += "_seconds"
        raw_name = (
            name.removesuffix("_seconds") if field_info["type"] == "duration" else name
        )
        lines.append(f"    if {quoted(field_info['name'])} in value:")
        lines.append(f"        {raw_name} = value[{quoted(field_info['name'])}]")
        field_type = field_info["type"]
        if field_type in ("string", "url"):
            check = f"isinstance({raw_name}, str)"
        elif field_type == "enum":
            check = f"isinstance({raw_name}, str) and {raw_name} in {tuple(field_info['enum'])!r}"
        elif field_type == "stringList":
            check = f"isinstance({raw_name}, list) and all(isinstance(item, str) for item in {raw_name})"
        elif field_type in ("integer", "duration"):
            check = f"type({raw_name}) is int"
        elif field_type == "number":
            check = f"type({raw_name}) in (int, float)"
        elif field_type == "boolean":
            check = f"type({raw_name}) is bool"
        else:
            raise ValueError(f"unsupported Python configuration type {field_type!r}")
        lines += [
            f"        if not ({check}):",
            f"            raise ValueError({quoted(connection_label + ' configuration ' + field_info['name'] + ' has an invalid type')})",
        ]
        expression = (
            f"{raw_name} / 1_000_000_000"
            if field_type == "duration"
            else f"tuple({raw_name})"
            if field_type == "stringList"
            else f"float({raw_name})"
            if field_type == "number"
            else raw_name
        )
        lines.append(f"        config_arguments[{name!r}] = {expression}")
        if "default" not in field_info:
            lines.append("    else:")
            if field_info["required"]:
                lines.append(
                    f"        raise ValueError({quoted(connection_label + ' configuration ' + field_info['name'] + ' is required')})"
                )
            else:
                fallback = {
                    "string": "''",
                    "url": "''",
                    "enum": "''",
                    "stringList": "()",
                    "integer": "0",
                    "number": "0.0",
                    "boolean": "False",
                    "duration": "0.0",
                }[field_type]
                lines.append(f"        config_arguments[{name!r}] = {fallback}")
    lines.append("    return Config(**config_arguments)")
    return "\n".join(lines).rstrip() + "\n"


class MockOutputExpression:
    def __init__(self, models):
        self.models_by_name = {model["name"]: model for model in models}

    def render(self, type_info, value):
        if value is None:
            return "None"
        kind = type_info["Kind"]
        if kind == "list":
            return (
                "("
                + ", ".join(self.render(type_info["Elem"], item) for item in value)
                + ("," if len(value) == 1 else "")
                + ")"
            )
        if kind == "map":
            return (
                "{"
                + ", ".join(
                    f"{key!r}: {self.render(type_info['Elem'], item)}"
                    for key, item in value.items()
                )
                + "}"
            )
        if kind == "time":
            return f"datetime.fromisoformat({quoted(value)}.replace('Z', '+00:00'))"
        if kind != "ref":
            return repr(value)
        model_name = type_info["Ref"]
        model = self.models_by_name.get(model_name)
        if model is None or not isinstance(value, dict):
            return repr(value)
        arguments = []
        by_lower_name = {key.lower(): key for key in value}
        for field_info in model["fields"]:
            wire_name = field_info["jsonName"]
            actual_key = (
                wire_name
                if wire_name in value
                else by_lower_name.get(wire_name.lower())
            )
            if actual_key is None:
                if not field_info["omitEmpty"] and not field_info["irType"]["Nullable"]:
                    if field_info["irType"]["Kind"] == "enum":
                        zero_value = (
                            "''"
                            if field_info["irType"]["EnumBase"] == "string"
                            else "0"
                        )
                        arguments.append(f"{python_field_name(wire_name)}={zero_value}")
                    elif field_info["irType"]["Kind"] == "ref":
                        arguments.append(
                            f"{python_field_name(wire_name)}={self.render(field_info['irType'], {})}"
                        )
                continue
            field_name = python_field_name(wire_name)
            field_value = self.render(field_info["irType"], value[actual_key])
            arguments.append(f"{field_name}={field_value}")
        return (
            model_aliases.get(model_name, model_name) + "(" + ", ".join(arguments) + ")"
        )


def render_mock():
    output_renderer = MockOutputExpression(models)
    operation_by_name = {
        snake(operation["name"]): operation for operation in operations
    }
    operation_prefixes = sorted(operation_by_name, key=len, reverse=True)
    scenarios = {}
    reserved_helpers = {
        snake(operation["name"]) + "_" + snake(branch["id"])
        for operation in operations
        for branch in operation["branches"]
    } | {snake(operation["name"]) + "_retry" for operation in operations}
    for operation in operations:
        operation_name = snake(operation["name"])
        for mock in operation.get("mocks", []):
            mock_name = snake(mock["name"])
            helper_name = (
                mock_name
                if mock_name.startswith(operation_name + "_")
                else operation_name + "_" + mock_name
            )
            unique_name = helper_name
            suffix = 0
            while unique_name in reserved_helpers:
                suffix += 1
                unique_name = (
                    helper_name + "_mock" + (str(suffix) if suffix > 1 else "")
                )
            reserved_helpers.add(unique_name)
            scenarios[operation["name"] + "." + mock["name"]] = unique_name
    scenario_by_helper = {helper: key for key, helper in scenarios.items()}
    helpers = []
    for operation in operations:
        helpers.extend(
            snake(operation["name"]) + "_" + snake(branch["id"])
            for branch in operation["branches"]
        )
        helpers.append(snake(operation["name"]) + "_retry")
        helpers.extend(
            scenarios[operation["name"] + "." + mock["name"]]
            for mock in operation.get("mocks", [])
        )
    mock_module = f"dex_connectors.{package_name}.{mock_package_name}"

    model_names = sorted(
        {
            model_aliases.get(name, name)
            for operation in operations
            for name in (operation["inputType"], operation["outputType"])
        }
        | {model_aliases.get(model["name"], model["name"]) for model in models}
    )
    kinds = {operation["kind"] for operation in operations}
    mock_imports = (
        ["Case"]
        + (["Mutation"] if "mutation" in kinds else [])
        + (["Query"] if "query" in kinds else [])
    )
    lines = [
        "# Copyright (c) 2026 Super Durable",
        "# SPDX-License-Identifier: MIT",
        "",
        "from datetime import datetime",
        "",
        "from dex_connectors.sdk import ConnectionRef, Failure, FailureKind",
        "from dex_connectors.sdk.connectormock import " + ", ".join(mock_imports),
        "",
        "from ..connection import Connection, new_connection_with_operations",
        "from ..models import " + ", ".join(model_names),
        "from .definitions import " + connection_definitions + ", "
        + ", ".join(
            sorted(
                {
                    constant(operation["name"]) + "_DEFINITION"
                    for operation in operations
                }
                | {
                    constant(branch["id"])
                    for operation in operations
                    for branch in operation["branches"]
                }
            )
        ),
        "",
    ]
    for helper_name in helpers:
        operation_prefix = next(
            (
                prefix
                for prefix in operation_prefixes
                if helper_name.startswith(prefix + "_")
            ),
            None,
        )
        if operation_prefix is None:
            raise ValueError(f"unknown Python mock helper {helper_name}")
        operation = operation_by_name[operation_prefix]
        output_type = operation["outputType"]
        if helper_name in scenario_by_helper:
            scenario_key = scenario_by_helper[helper_name]
            operation_name, mock_name = scenario_key.split(".", 1)
            if operation_name != operation["name"]:
                raise ValueError(
                    f"Python mock scenario {scenario_key} has wrong operation"
                )
            mock = next(
                (
                    mock
                    for mock in operation.get("mocks", [])
                    if mock["name"] == mock_name
                ),
                None,
            )
            if mock is None:
                raise ValueError(f"stale Python mock scenario {scenario_key}")
            lines += [
                f"def {helper_name}() -> Case[{output_type}]:",
            ]
            failure = (
                mock.get("failure")
                or mock.get("retry", {}).get("failure")
                or mock.get("uncertain", {}).get("failure")
            )
            failure_expr = None
            if failure:
                failure_expr = f"Failure(FailureKind.{failure['kind']}, CONNECTOR_ID, {quoted(operation['name'])}, {quoted(failure['message'])})"
            if mock.get("retry") is not None:
                after = mock["retry"].get("after", "")
                after_expr = (
                    duration(after).replace("timedelta(seconds=", "").rstrip(")")
                    if after
                    else "0"
                )
                lines.append(f"    return Case.retry({failure_expr}, {after_expr})")
            elif mock.get("uncertain") is not None:
                value = output_renderer.render(
                    {"Kind": "ref", "Ref": output_type},
                    mock["uncertain"].get("output", {}),
                )
                lines.append(f"    return Case.uncertain({value}, {failure_expr})")
            elif mock.get("pages"):
                pages = [
                    output_renderer.render({"Kind": "ref", "Ref": output_type}, page)
                    for page in mock["pages"]
                ]
                lines.append(
                    f"    return Case.paged({constant(mock['branch'])}, ({', '.join(pages)}{',' if len(pages) == 1 else ''}))"
                )
            else:
                value = output_renderer.render(
                    {"Kind": "ref", "Ref": output_type}, mock.get("output", {})
                )
                if failure_expr:
                    lines.append(
                        f"    return Case.branch_result({constant(mock['branch'])}, {value}, {failure_expr})"
                    )
                else:
                    lines.append(
                        f"    return Case.branch_result({constant(mock['branch'])}, {value})"
                    )
        else:
            branch_name = helper_name[len(operation_prefix) + 1 :]
            if branch_name == "retry":
                lines += [
                    f"def {helper_name}(failure: Failure, after_seconds: float = 0) -> Case[{output_type}]:",
                    "    return Case.retry(failure, after_seconds)",
                ]
            else:
                branch = next(
                    (
                        branch
                        for branch in operation["branches"]
                        if snake(branch["id"]) == branch_name
                    ),
                    None,
                )
                if branch is None:
                    raise ValueError(f"unknown Python mock branch helper {helper_name}")
                required_failure = branch["id"] == "uncertain"
                failure_parameter = (
                    "failure: Failure"
                    if required_failure
                    else "failure: Failure | None = None"
                )
                lines += [
                    f"def {helper_name}(value: {output_type}, {failure_parameter}) -> Case[{output_type}]:",
                ]
                if required_failure:
                    lines.append("    return Case.uncertain(value, failure)")
                else:
                    lines.append(
                        f"    return Case.branch_result({constant(branch['id'])}, value, failure)"
                    )
        lines += [
            "",
            f"setattr({helper_name}, '__module__', {quoted(mock_module)})",
            "",
        ]
    connection_default = connector_id
    lines += [
        "",
        "class Mock:",
        "",
        f"    def __init__(self, connection_name: str = {quoted(connection_default)}) -> None:",
    ]
    for operation in operations:
        operation_name = snake(operation["name"])
        kind = "Mutation" if operation["kind"] == "mutation" else "Query"
        lines.append(
            f"        self.{operation_name}_script: {kind}[{operation['inputType']}, {operation['outputType']}] = {kind}({constant(operation['name'])}_DEFINITION)"
        )
        default_mock = next(
            (mock for mock in operation.get("mocks", []) if mock.get("default")), None
        )
        if default_mock:
            helper_name = scenarios[operation["name"] + "." + default_mock["name"]]
            lines.append(
                f"        self.{operation_name}_script.default({helper_name}())"
            )
    lines += ["        self._connection = new_connection_with_operations("]
    for operation in operations:
        operation_name = snake(operation["name"])
        lines.append(f"            {operation_name}=self.{operation_name}_script,")
    lines += [
        f"            reference=ConnectionRef({connection_provider}, connection_name),",
        "        )",
        "",
        "    def connection(self) -> Connection:",
        "        return self._connection",
    ]
    lines += [
        "",
        f"setattr(Mock, '__module__', {quoted(mock_module)})",
        "",
        "__all__ = [",
    ]
    for name in ["Mock"] + helpers:
        lines.append(f"    {quoted(name)},")
    lines += ["]", ""]
    return "\n".join(lines).rstrip() + "\n"


result = {
    "__init__.py": "# Copyright (c) 2026 Super Durable\n# SPDX-License-Identifier: MIT\n",
    "models.py": render_models(),
    "config.py": render_config(),
    "definitions.py": render_definitions(),
    "steps.py": render_steps(),
}
if payload["hasDeclaredTokenGrants"]:
    authorization = json.loads(json.dumps(spec["auth"]))
    omitted = {
        "description", "displayName", "goName", "guide", "connectionKind",
        "recommended", "methodLabel", "configuration", "authorizationEndpoint",
        "authorizationParameters", "credentialDerivations", "pkce", "oidc", "protocol",
    }
    def trim_refresh_metadata(value):
        if isinstance(value, dict):
            structural = {
                "methods", "fields", "oauth2", "clientCredentials", "serviceAccount",
                "refresh", "grants", "jwtBearer",
            }
            return {
                name: trim_refresh_metadata(item) if name in structural else item
                for name, item in value.items() if name not in omitted
            }
        if isinstance(value, list):
            return [trim_refresh_metadata(item) for item in value]
        return value
    authorization = trim_refresh_metadata(authorization)
    if auth_methods:
        authorization.pop("fields", None)
    result["refresh.py"] = (
        "# Copyright (c) 2026 Super Durable\n# SPDX-License-Identifier: MIT\n\n"
        "# Validated manifest token grant declaration.\n"
        f"AUTHORIZATION = {authorization!r}\n\n"
        "# Stored credential names mapped to runtime dataclass fields.\n"
        f"CREDENTIAL_FIELDS = { {field_info['name']: snake(field_info['name']) for field_info in auth_fields()}!r}\n"
    )
connection_source = render_connection()
if connection_source is not None:
    result["connection.py"] = connection_source
mock_source = render_mock()
if mock_source is not None:
    result["mock.py"] = mock_source
for filename, source in result.items():
    if filename != "__init__.py":
        source = source.replace(
            "# SPDX-License-Identifier: MIT\n",
            "# SPDX-License-Identifier: MIT\n# ruff: noqa\n",
            1,
        )
    result[filename] = source
json.dump(result, sys.stdout)
