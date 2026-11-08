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


# ListLeadFormsInput selects a Facebook Page and an optional pagination cursor.
@dataclass(frozen=True, slots=True)
class ListLeadFormsInput:
    # PageID is the numeric Page ID from Meta Business Suite.
    page_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageId'})
    # After is Meta's opaque cursor from a prior page; blank starts at the first page.
    after: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListLeadFormsInput, '__module__', 'dex_connectors.meta_facebook_lead_ads.models')


# LeadFormMetadata contains a form's public identity and status.
@dataclass(frozen=True, slots=True)
class LeadFormMetadata:
    # ID is the stable Meta lead-form ID.
    id: str = ''
    # Name is the untrusted display name.
    name: str = ''
    # Status is Meta's form status.
    status: str = ''


setattr(LeadFormMetadata, '__module__', 'dex_connectors.meta_facebook_lead_ads.models')


# LeadFormsPage contains one bounded page of lead forms.
@dataclass(frozen=True, slots=True)
class LeadFormsPage:
    # Forms are the form identities in provider order.
    forms: tuple[LeadFormMetadata, ...] = ()
    # NextCursor is blank when Meta reported no later page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'forms', _replace_nil_slices(self.forms, 1))


LeadFormsPage.__annotations__['forms'] = tuple[LeadFormMetadata, ...] | None
LeadFormsPage.__dataclass_fields__['forms'].type = tuple[LeadFormMetadata, ...] | None
setattr(LeadFormsPage, '__module__', 'dex_connectors.meta_facebook_lead_ads.models')


# GetLeadInput selects one Meta lead.
@dataclass(frozen=True, slots=True)
class GetLeadInput:
    # LeadID is the numeric ID from a leadgen webhook or Ads Manager.
    lead_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'leadId'})


setattr(GetLeadInput, '__module__', 'dex_connectors.meta_facebook_lead_ads.models')


# LeadMetadata excludes form answers, which may contain sensitive personal data.
@dataclass(frozen=True, slots=True)
class LeadMetadata:
    # ID is the stable Meta lead ID.
    id: str = ''
    # FormID identifies the originating lead form.
    form_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'formId'})
    # AdID identifies the originating ad when present.
    ad_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'adId'})
    # CreatedTime is Meta's raw lead creation timestamp.
    created_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'createdTime'})


setattr(LeadMetadata, '__module__', 'dex_connectors.meta_facebook_lead_ads.models')
