# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

import json
from dataclasses import MISSING, fields, is_dataclass
from datetime import datetime
from types import NoneType, UnionType
from typing import Any, Union, get_args, get_origin, get_type_hints

from dex import CodecRegistry, Value, WireKind
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    TriggerBindingDefinition,
    must_trigger_binding_definition,
)

from ..models import TicketChangedTriggerConfiguration
from . import models as _generated_models
from .definitions import TICKET_CHANGED_TRIGGER_DEFINITION


def _allows_none(annotation: Any) -> bool:
    return annotation is Any or (
        get_origin(annotation) in (Union, UnionType) and NoneType in get_args(annotation)
    )


# Checks one stored JSON value against a configuration annotation, as Go strictJSON does.
def _strict_configuration(trigger: str, value: Any, annotation: Any, path: str) -> Any:
    def invalid(problem: str) -> ValueError:
        return ValueError(f"{trigger} {path} {problem}")

    if annotation is Any:
        return value
    origin = get_origin(annotation)
    if origin in (Union, UnionType):
        if value is None:
            return None
        options = [option for option in get_args(annotation) if option is not NoneType]
        return _strict_configuration(trigger, value, options[0], path) if len(options) == 1 else value
    if value is None:
        raise invalid("must not be null")
    if is_dataclass(annotation):
        if not isinstance(value, dict):
            raise invalid("must be an object")
        hints = get_type_hints(annotation, localns=vars(_generated_models))
        by_wire_name = {item.metadata.get("dex_json_name", item.name): item for item in fields(annotation)}
        unknown = sorted(set(value) - set(by_wire_name))
        if unknown:
            raise invalid(f"has unknown field {unknown[0]!r}")
        checked = {}
        for wire_name, item in value.items():
            field_info = by_wire_name[wire_name]
            if item is None and not _allows_none(hints[field_info.name]):
                # Go leaves the zero value for null; a field without a default has none.
                if field_info.default is MISSING and field_info.default_factory is MISSING:
                    raise ValueError(f"{trigger} {path}.{wire_name} must not be null")
                continue
            checked[wire_name] = _strict_configuration(trigger, item, hints[field_info.name], f"{path}.{wire_name}")
        return checked
    if origin in (tuple, list):
        if not isinstance(value, list):
            raise invalid("must be a list")
        element = get_args(annotation)[0]
        return [_strict_configuration(trigger, item, element, f"{path}[{index}]") for index, item in enumerate(value)]
    if origin is dict:
        if not isinstance(value, dict):
            raise invalid("must be an object")
        element = get_args(annotation)[1]
        return {key: _strict_configuration(trigger, item, element, f"{path}.{key}") for key, item in value.items()}
    if annotation is bool and not isinstance(value, bool):
        raise invalid("must be a boolean")
    if annotation is int and (isinstance(value, bool) or not isinstance(value, int)):
        raise invalid("must be an integer")
    if annotation is float and (isinstance(value, bool) or not isinstance(value, (int, float))):
        raise invalid("must be a number")
    if annotation in (str, datetime) and not isinstance(value, str):
        raise invalid("must be a string")
    return value


def define_ticket_changed_trigger_binding(
    *,
    connection_name: str,
    binding_name: str,
    configuration_ui: ConnectorConfigurationUI | None = None,
) -> TriggerBindingDefinition:
    return must_trigger_binding_definition(
        TriggerBindingDefinition(
            definition=TICKET_CHANGED_TRIGGER_DEFINITION,
            connection_name=connection_name,
            binding_name=binding_name,
            configuration_ui=configuration_ui,
        )
    )

setattr(define_ticket_changed_trigger_binding, '__module__', 'dex_connectors.zoho_desk.triggers')


def decode_ticket_changed_trigger_configuration(value: dict[str, Any]) -> TicketChangedTriggerConfiguration:
    checked = _strict_configuration("ticketChanged", value, TicketChangedTriggerConfiguration, 'configuration')
    try:
        decoded = CodecRegistry().resolve(TicketChangedTriggerConfiguration).decode(Value(WireKind.JSON, json.dumps(checked)))
    except (TypeError, ValueError) as error:
        raise ValueError("ticketChanged configuration did not decode") from error
    if not isinstance(decoded, TicketChangedTriggerConfiguration):
        raise ValueError("ticketChanged configuration did not decode")
    return decoded

setattr(decode_ticket_changed_trigger_configuration, '__module__', 'dex_connectors.zoho_desk.triggers')
