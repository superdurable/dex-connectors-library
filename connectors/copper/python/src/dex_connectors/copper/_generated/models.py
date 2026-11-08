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


# ListLeadStatusesInput selects lead statuses visible to the connected Copper user.
@dataclass(frozen=True, slots=True)
class ListLeadStatusesInput:
    pass


setattr(ListLeadStatusesInput, '__module__', 'dex_connectors.copper.models')


# LeadStatusMetadata identifies one Copper lead status without private fields.
@dataclass(frozen=True, slots=True)
class LeadStatusMetadata:
    # ID is the stable Copper status identifier.
    id: int = 0
    # Name is the status display name.
    name: str = ''


setattr(LeadStatusMetadata, '__module__', 'dex_connectors.copper.models')


# LeadStatusesList contains visible lead status identities.
@dataclass(frozen=True, slots=True)
class LeadStatusesList:
    # Statuses contains only IDs and names.
    statuses: tuple[LeadStatusMetadata, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'statuses', _replace_nil_slices(self.statuses, 1))


LeadStatusesList.__annotations__['statuses'] = tuple[LeadStatusMetadata, ...] | None
LeadStatusesList.__dataclass_fields__['statuses'].type = tuple[LeadStatusMetadata, ...] | None
setattr(LeadStatusesList, '__module__', 'dex_connectors.copper.models')


# GetLeadInput selects a Copper lead by its numeric ID.
@dataclass(frozen=True, slots=True)
class GetLeadInput:
    # LeadID is the numeric ID shown on a Copper lead and returned by its API.
    lead_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'leadId'})


setattr(GetLeadInput, '__module__', 'dex_connectors.copper.models')


# LeadMetadata contains the lead's ID, name, status, and company name.
@dataclass(frozen=True, slots=True)
class LeadMetadata:
    # ID is the stable Copper lead identifier.
    id: int = 0
    # Name is the lead display name.
    name: str = ''
    # Status is the current lead status name.
    status: str = ''
    # CompanyName is the associated company name when available.
    company_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'companyName'})


setattr(LeadMetadata, '__module__', 'dex_connectors.copper.models')
