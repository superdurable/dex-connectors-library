# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# CheckIPReputationInput identifies one inbound IPv4 or IPv6 source address.
@dataclass(frozen=True, slots=True)
class CheckIPReputationInput:
    # IPAddress is a single IPv4 or IPv6 address from an inbound access event.
    ip_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'ipAddress'})


setattr(CheckIPReputationInput, '__module__', 'dex_connectors.threatbook.models')


# CheckIPReputationOutput contains only the main verdict and limited threat labels.
@dataclass(frozen=True, slots=True)
class CheckIPReputationOutput:
    # IsMalicious is ThreatBook's inbound-risk verdict.
    is_malicious: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isMalicious'})
    # ConfidenceLevel is low, medium, or high when supplied.
    confidence_level: str = _dataclass_field(default='', metadata={'dex_json_name': 'confidenceLevel'})
    # Severity is critical, high, medium, low, or info when supplied.
    severity: str = ''
    # Judgments are at most twenty threat-type labels.
    judgments: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'judgments', _replace_nil_slices(self.judgments, 1))


CheckIPReputationOutput.__annotations__['judgments'] = tuple[str, ...] | None
CheckIPReputationOutput.__dataclass_fields__['judgments'].type = tuple[str, ...] | None
setattr(CheckIPReputationOutput, '__module__', 'dex_connectors.threatbook.models')
