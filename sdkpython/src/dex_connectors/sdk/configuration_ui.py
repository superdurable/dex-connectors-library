# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Connector-owned Dex Web configuration presentation metadata."""

import re
from dataclasses import dataclass

_IDENTIFIER = re.compile(r"^[a-z][A-Za-z0-9]*$")


@dataclass(frozen=True, slots=True)
class ConnectorUIBinding:
    """Map one released UI unit port to an application JSON Pointer."""

    port: str
    json_pointer: str

    def validate(self) -> None:
        """Require a lower-camel port and valid nonempty RFC 6901 path."""
        if not _IDENTIFIER.fullmatch(self.port) or not self.json_pointer.startswith("/"):
            raise ValueError("invalid Connector UI binding")
        index = 0
        while index < len(self.json_pointer):
            if self.json_pointer[index] == "~":
                if index + 1 >= len(self.json_pointer) or self.json_pointer[index + 1] not in "01":
                    raise ValueError("invalid Connector UI JSON Pointer escape")
                index += 1
            index += 1

    def to_wire(self) -> dict[str, str]:
        """Encode the released binding shape."""
        self.validate()
        return {"port": self.port, "jsonPointer": self.json_pointer}


@dataclass(frozen=True, slots=True)
class ConnectorUIUnit:
    """One reusable catalog unit in application composition order."""

    id: str
    unit_id: str
    label: str
    description: str
    required: bool
    bindings: tuple[ConnectorUIBinding, ...]

    def validate(self) -> None:
        """Check static unit guidance and unique ports."""
        if not _IDENTIFIER.fullmatch(self.id) or not _IDENTIFIER.fullmatch(self.unit_id):
            raise ValueError("Connector UI unit IDs must be lower camel case")
        if not self.label.strip() or not self.description.strip() or not self.bindings:
            raise ValueError("Connector UI unit needs a label, description, and bindings")
        ports = [binding.port for binding in self.bindings]
        if len(ports) != len(set(ports)):
            raise ValueError("Connector UI unit ports must be unique")
        for binding in self.bindings:
            binding.validate()

    def to_wire(self) -> dict[str, object]:
        """Encode the released unit shape."""
        self.validate()
        return {
            "id": self.id,
            "unitId": self.unit_id,
            "label": self.label,
            "description": self.description,
            "required": self.required,
            "bindings": [binding.to_wire() for binding in self.bindings],
        }


@dataclass(frozen=True, slots=True)
class ConnectorConfigurationUI:
    """Ordered connector units shown for a Step or Trigger binding."""

    units: tuple[ConnectorUIUnit, ...]

    def validate(self) -> None:
        """Check all units and prevent duplicate use IDs."""
        ids = [unit.id for unit in self.units]
        if len(ids) != len(set(ids)):
            raise ValueError("Connector UI unit IDs must be unique")
        for unit in self.units:
            unit.validate()

    def to_wire(self) -> dict[str, object]:
        """Encode the released configuration UI shape."""
        self.validate()
        return {"units": [unit.to_wire() for unit in self.units]}


@dataclass(frozen=True, slots=True)
class ConnectorConfigurationRef:
    """Stable Flow and Step use that owns an operation configuration."""

    connector_id: str
    connection_name: str
    operation_id: str
    flow_type: str
    step_type: str

    def validate(self) -> None:
        """Require stable connector, operation, connection, Flow, and Step identities."""
        from .types import CONNECTOR_ID_PATTERN, OPERATION_ID_PATTERN

        if not CONNECTOR_ID_PATTERN.fullmatch(self.connector_id) or not OPERATION_ID_PATTERN.fullmatch(
            self.operation_id
        ):
            raise ValueError("invalid Connector configuration identity")
        if not self.connection_name.strip() or not self.flow_type.strip() or not self.step_type.strip():
            raise ValueError("Connector configuration connection, Flow, and Step are required")
