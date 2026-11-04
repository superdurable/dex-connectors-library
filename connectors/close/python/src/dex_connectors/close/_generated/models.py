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


# ListLeadsInput selects one Close lead page.
@dataclass(frozen=True, slots=True)
class ListLeadsInput:
    # Skip is the zero-based offset into the lead list.
    skip: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Limit caps leads returned; zero uses 100.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListLeadsInput, '__module__', 'dex_connectors.close.models')


# Lead identifies a Close lead and its display name.
@dataclass(frozen=True, slots=True)
class Lead:
    # ID is the provider-assigned resource identity.
    id: str = ''
    # Name is the resource display name.
    name: str = ''


setattr(Lead, '__module__', 'dex_connectors.close.models')


# LeadPage contains one page and a continuation flag.
@dataclass(frozen=True, slots=True)
class LeadPage:
    # Leads contains the returned Close lead records.
    leads: tuple[Lead, ...] = ()
    # HasMore reports whether another lead page exists.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'leads', _replace_nil_slices(self.leads, 1))


LeadPage.__annotations__['leads'] = tuple[Lead, ...] | None
LeadPage.__dataclass_fields__['leads'].type = tuple[Lead, ...] | None
setattr(LeadPage, '__module__', 'dex_connectors.close.models')


# GetLeadInput identifies one Close lead.
@dataclass(frozen=True, slots=True)
class GetLeadInput:
    # LeadID is the Close lead_ identifier.
    lead_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'leadId'})


setattr(GetLeadInput, '__module__', 'dex_connectors.close.models')
