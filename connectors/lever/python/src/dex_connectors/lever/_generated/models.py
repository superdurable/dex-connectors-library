# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetOpportunityInput selects an opportunity by its non-secret Lever ID.
@dataclass(frozen=True, slots=True)
class GetOpportunityInput:
    # OpportunityID is the ID shown in Lever Opportunities or returned by the API.
    opportunity_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'opportunityId'})


setattr(GetOpportunityInput, '__module__', 'dex_connectors.lever.models')


# OpportunityStage excludes contact details, resumes, and notes.
@dataclass(frozen=True, slots=True)
class OpportunityStage:
    # ID is the Lever opportunity UID.
    id: str = ''
    # Name is the candidate display name.
    name: str = ''
    # Stage is the current pipeline stage UID.
    stage: str = ''


setattr(OpportunityStage, '__module__', 'dex_connectors.lever.models')
