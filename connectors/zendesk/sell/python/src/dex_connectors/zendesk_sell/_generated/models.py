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


# ListLeadsInput selects one bounded Zendesk Sell lead page.
@dataclass(frozen=True, slots=True)
class ListLeadsInput:
    # Page is the one-based page number.
    page: int = 0
    # PerPage is the number of leads to return, from 1 to 100.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage'})


setattr(ListLeadsInput, '__module__', 'dex_connectors.zendesk_sell.models')


# LeadMetadata contains lead identity without custom fields or links.
@dataclass(frozen=True, slots=True)
class LeadMetadata:
    # ID is the stable Zendesk Sell lead identifier.
    id: int = 0
    # FirstName is the lead's given name when available.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName'})
    # LastName is the lead's family name when available.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName'})
    # EmailAddress is the lead's email address when available.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress'})
    # OrganizationName is the lead's organization when available.
    organization_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'organizationName'})


setattr(LeadMetadata, '__module__', 'dex_connectors.zendesk_sell.models')


# LeadsPage contains one requested page of lead identities.
@dataclass(frozen=True, slots=True)
class LeadsPage:
    # Leads contains only identity fields from the provider response.
    leads: tuple[LeadMetadata, ...] = ()
    # Page is the one-based page number requested from the provider.
    page: int = 0
    # PerPage is the requested maximum page size.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'leads', _replace_nil_slices(self.leads, 1))


LeadsPage.__annotations__['leads'] = tuple[LeadMetadata, ...] | None
LeadsPage.__dataclass_fields__['leads'].type = tuple[LeadMetadata, ...] | None
setattr(LeadsPage, '__module__', 'dex_connectors.zendesk_sell.models')


# GetLeadInput selects a lead by its positive numeric ID.
@dataclass(frozen=True, slots=True)
class GetLeadInput:
    # LeadID is the decimal ID shown in Zendesk Sell and its API.
    lead_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'leadId'})


setattr(GetLeadInput, '__module__', 'dex_connectors.zendesk_sell.models')
