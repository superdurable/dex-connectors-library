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
operations_by_name = {operation["name"]: operation for operation in spec["operations"]}
operations = [
    operations_by_name[name] for name in spec["languages"]["python"]["operations"]
]
connector_id = metadata["name"]
package_name = payload["packageName"]
mock_package_name = payload["mockPackageName"]


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
    for model in payload["models"]
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
    if type_info["Nullable"] or has_presence or (optional and kind in ("enum", "ref")):
        if kind != "unknown":
            if kind == "ref" and type_info["Ref"] not in defined_models:
                annotation = f"Optional[{annotation}]"
            else:
                annotation += " | None"
        default = "None"
    return annotation, default


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
    defined_models = set()
    for model in dependency_order.ordered:
        description = model.get("description", "").strip()
        if description:
            lines.extend("# " + line for line in description.splitlines())
        lines += ["@dataclass(frozen=True, slots=True)", f"class {model['name']}:"]
        if not model["fields"]:
            lines.append("    pass")
        has_default_field = False
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
        lines += ["", ""]
        lines.append(
            f"setattr({model['name']}, '__module__', 'dex_connectors.{package_name}.models')"
        )
        lines += ["", ""]
        defined_models.add(model["name"])
    body = "\n".join(lines).rstrip() + "\n"
    header = [
        "# Copyright (c) 2026 Super Durable",
        "# SPDX-License-Identifier: MIT",
        '"""Generated Python dataclasses from Go operation types."""',
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
        '"""Generated operation identities, branches, and Step defaults."""',
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
        '"""Generated connection settings and credential values."""',
        "",
        "from dataclasses import dataclass",
    ]
    credential_fields = spec["auth"].get("fields", [])
    if any(field["type"] == "secretString" for field in credential_fields):
        lines += ["", "from dex_connectors.sdk import SecretString"]
    lines += [
        "",
        "",
        "@dataclass(frozen=True, slots=True)",
        "class Config:",
        f'    """{metadata["displayName"]} connection settings from the manifest."""',
        "",
    ]
    config_fields = spec["configuration"]["fields"]
    config_fields = sorted(
        config_fields, key=lambda field_info: "default" in field_info
    )
    if not config_fields:
        lines.append("    pass")
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
        else:
            field_type = "str"
            default = quoted(field_info["default"]) if "default" in field_info else None
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
            f'    """{metadata["displayName"]} provider credentials resolved per call."""',
            "",
        ]
        for field_info in credential_fields:
            field_type = (
                "SecretString" if field_info["type"] == "secretString" else "str"
            )
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
        '"""Generated operation-specific Dex Step factories."""',
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
            f'    """Create the {name} Step with the manifest branch identities."""',
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
        '    """Resolve a bound operation from a connection or its operation collection."""',
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
    lines = [
        "# Copyright (c) 2026 Super Durable",
        "# SPDX-License-Identifier: MIT",
        '"""Generated operation bindings for one named Dex connection."""',
        "",
        "from typing import Any",
        "",
        "import httpx",
        "from dex_connectors.sdk import "
        + ", ".join(
            imports + (["Call", "SecretString"] if spec["auth"].get("fields") else [])
        ),
        "from dex_connectors.sdk.projectconfig import "
        + (
            "ConnectionKey, LoadedProject"
            if spec["auth"].get("fields")
            else "LoadedProject"
        ),
        "",
        "from ..client import Client, Config"
        + (", Credentials" if spec["auth"].get("fields") else ""),
        "from .definitions import CONNECTOR_ID, " + ", ".join(definition_names),
        "from ..models import " + ", ".join(model_names),
        "",
        "class Connection:",
        f'    """Nonserializable {metadata["displayName"]} operation binding."""',
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
    checks = ["reference.provider != CONNECTOR_ID"] + [
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
            f'        """Return the {name} operation for a Dex Step."""',
            f"        return self._{name}",
            "",
        ]
    lines += [
        "    async def aclose(self) -> None:",
        '        """Close the owned provider transport after Worker shutdown."""',
        "        if self._client is not None:",
        "            await self._client.aclose()",
        "",
        "    def __repr__(self) -> str:",
        '        """Keep provider transport and credentials out of logs."""',
        f"        return {quoted(package_name + '.Connection([REDACTED])')}",
        "",
        "    def __reduce__(self) -> tuple[object, ...]:",
        '        """Prevent a live connection from entering durable Flow state."""',
        f"        raise TypeError({quoted(connection_label + ' connections cannot be serialized')})",
        "",
        f"setattr(Connection, '__module__', 'dex_connectors.{package_name}.connection')",
        "",
        "",
        "def new_connection(client: Client, reference: ConnectionRef) -> Connection:",
        '    """Bind a provider client to an explicit logical connection reference."""',
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
        '    """Bind mock operations to the same logical connection identity."""',
        "    return Connection(reference, " + ", ".join(name for name in order) + ")",
        "",
        f"setattr(new_connection_with_operations, '__module__', 'dex_connectors.{package_name}.connection')",
        "",
    ]
    credential_fields = spec["auth"].get("fields", [])
    if credential_fields:
        lines += [
            "",
            "class _ProjectCredentialSource:",
            '    """Resolve current secret material at each provider attempt."""',
            "",
            "    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:",
            "        self.project = project",
            "        self.key = key",
            "",
            "    async def resolve(self, call: Call) -> Credentials:",
            "        material = await self.project.resolve_credential(self.key, str(call.id))",
        ]
        for field_info in credential_fields:
            name = snake(field_info["name"])
            lines += [
                f"        {name} = material.get({quoted(field_info['name'])})",
                f"        if not isinstance({name}, str) or not {name}:",
                f"            raise ValueError({quoted(connection_label + ' credential ' + field_info['name'] + ' is required')})",
            ]
        values = [
            f"SecretString({snake(field_info['name'])})"
            if field_info["type"] == "secretString"
            else snake(field_info["name"])
            for field_info in credential_fields
        ]
        lines += ["        return Credentials(" + ", ".join(values) + ")", ""]
    lines += [
        "",
        "def new_project_connection(",
        "    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None",
        ") -> Connection:",
        '    """Open declared project settings and resolve provider credentials per call."""',
        "    if project is None:",
        f"        raise ValueError({quoted(connection_label + ' connection requires loaded project configuration')})",
        "    reference = ConnectionRef(CONNECTOR_ID, connection_name)",
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
        "    if matching[0].provider != CONNECTOR_ID:",
        f"        raise ValueError({quoted(connection_label + ' project connection provider differs')})",
        "    config = _decode_config(matching[0].configuration)",
    ]
    client_args = (
        "config, _ProjectCredentialSource(project, key), http_client"
        if credential_fields
        else "config, http_client"
    )
    lines += [
        f"    client = Client({client_args})",
        "    return new_connection(client, reference)",
        "",
        f"setattr(new_project_connection, '__module__', 'dex_connectors.{package_name}.connection')",
        "",
        "",
        "def _decode_config(value: dict[str, Any]) -> Config:",
    ]
    config_fields = spec["configuration"]["fields"]
    allowed = (
        "{"
        + ", ".join(quoted(field_info["name"]) for field_info in config_fields)
        + "}"
    )
    lines += [
        f"    if set(value) - {allowed}:",
        f"        raise ValueError({quoted(connection_label + ' configuration has unknown fields')})",
    ]
    config_names = []
    for field_info in config_fields:
        name = snake(field_info["name"])
        if field_info["type"] == "duration":
            name += "_seconds"
        config_names.append(name)
        raw_name = (
            name.removesuffix("_seconds") if field_info["type"] == "duration" else name
        )
        default = (
            "''"
            if field_info["type"] in ("string", "url")
            else "False"
            if field_info["type"] == "boolean"
            else "0"
        )
        lines.append(
            f"    {raw_name} = value.get({quoted(field_info['name'])}, {default})"
        )
        check = (
            f"isinstance({raw_name}, str)"
            if field_info["type"] in ("string", "url")
            else f"type({raw_name}) is {'bool' if field_info['type'] == 'boolean' else 'int'}"
        )
        lines += [
            f"    if not {check}:",
            f"        raise ValueError({quoted(connection_label + ' configuration ' + field_info['name'] + ' has an invalid type')})",
        ]
        if field_info["type"] == "duration":
            lines.append(f"    {name} = {raw_name} / 1_000_000_000")
    by_field = {
        field_info["name"]: snake(field_info["name"])
        + ("_seconds" if field_info["type"] == "duration" else "")
        for field_info in config_fields
    }
    lines.append(
        "    return Config("
        + ", ".join(f"{by_field[name]}={by_field[name]}" for name in by_field)
        + ")"
    )
    return "\n".join(lines).rstrip() + "\n"


def render_mock():
    model_by_name = {model["name"]: model for model in models}
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

    def output_expression(type_info, value):
        if value is None:
            return "None"
        kind = type_info["Kind"]
        if kind == "list":
            return (
                "("
                + ", ".join(
                    output_expression(type_info["Elem"], item) for item in value
                )
                + ("," if len(value) == 1 else "")
                + ")"
            )
        if kind == "map":
            return (
                "{"
                + ", ".join(
                    f"{key!r}: {output_expression(type_info['Elem'], item)}"
                    for key, item in value.items()
                )
                + "}"
            )
        if kind == "time":
            return f"datetime.fromisoformat({quoted(value)}.replace('Z', '+00:00'))"
        if kind != "ref":
            return repr(value)
        model_name = type_info["Ref"]
        model = model_by_name.get(model_name)
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
                            f"{python_field_name(wire_name)}={output_expression(field_info['irType'], {})}"
                        )
                continue
            field_name = python_field_name(wire_name)
            field_value = output_expression(field_info["irType"], value[actual_key])
            arguments.append(f"{field_name}={field_value}")
        return model_name + "(" + ", ".join(arguments) + ")"

    model_names = sorted(
        {
            name
            for operation in operations
            for name in (operation["inputType"], operation["outputType"])
        }
        | {model["name"] for model in models}
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
        '"""Generated manifest-backed provider outcomes and scripts."""',
        "",
        "from datetime import datetime",
        "",
        "from dex_connectors.sdk import ConnectionRef, Failure, FailureKind",
        "from dex_connectors.sdk.connectormock import " + ", ".join(mock_imports),
        "",
        "from ..connection import Connection, new_connection_with_operations",
        "from ..models import " + ", ".join(model_names),
        "from .definitions import CONNECTOR_ID, "
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
                f'    """Return the manifest {mock_name} outcome."""',
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
                value = output_expression(
                    {"Kind": "ref", "Ref": output_type},
                    mock["uncertain"].get("output", {}),
                )
                lines.append(f"    return Case.uncertain({value}, {failure_expr})")
            elif mock.get("pages"):
                pages = [
                    output_expression({"Kind": "ref", "Ref": output_type}, page)
                    for page in mock["pages"]
                ]
                lines.append(
                    f"    return Case.paged({constant(mock['branch'])}, ({', '.join(pages)}{',' if len(pages) == 1 else ''}))"
                )
            else:
                value = output_expression(
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
                    '    """Script a retry of this provider operation."""',
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
                    f'    """Script the {branch["id"]} branch."""',
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
        '    """Script manifest outcomes for this connector."""',
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
        "            reference=ConnectionRef(CONNECTOR_ID, connection_name),",
        "        )",
        "",
        "    def connection(self) -> Connection:",
        '        """Return this mock connection for Dex Step registration."""',
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
    "__init__.py": '# Copyright (c) 2026 Super Durable\n# SPDX-License-Identifier: MIT\n"""Manifest-generated Python connector contract."""\n',
    "models.py": render_models(),
    "config.py": render_config(),
    "definitions.py": render_definitions(),
    "steps.py": render_steps(),
}
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
    result[filename] = (
        "\n".join(
            line
            for line in source.splitlines()
            if not re.fullmatch(r'\s*"""[^"\n]*"""', line)
        ).rstrip()
        + "\n"
    )
json.dump(result, sys.stdout)
