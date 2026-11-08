# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetLeadInput selects one non-secret positive Marketo lead ID.
@dataclass(frozen=True, slots=True)
class GetLeadInput:
    # LeadID is the ID shown on a lead detail page or returned by a lead query.
    lead_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'leadId'})


setattr(GetLeadInput, '__module__', 'dex_connectors.adobe_marketo.models')


# LeadProfile contains the selected ID and optional names, excluding email and other fields.
@dataclass(frozen=True, slots=True)
class LeadProfile:
    # ID is the Marketo lead ID.
    id: int = 0
    # FirstName is the lead's first name, or empty when absent.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName'})
    # LastName is the lead's last name, or empty when absent.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName'})


setattr(LeadProfile, '__module__', 'dex_connectors.adobe_marketo.models')
