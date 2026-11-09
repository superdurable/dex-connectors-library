# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetCandidateInput selects a candidate by its non-secret Ashby ID.
@dataclass(frozen=True, slots=True)
class GetCandidateInput:
    # CandidateID is the ID shown in Ashby Candidates or returned by the API.
    candidate_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'candidateId'})


setattr(GetCandidateInput, '__module__', 'dex_connectors.ashby.models')


# CandidateProfile excludes email, phone, files, and custom fields.
@dataclass(frozen=True, slots=True)
class CandidateProfile:
    # ID is the Ashby candidate UUID.
    id: str = ''
    # Name is the candidate display name.
    name: str = ''
    # CreatedAt is Ashby's creation timestamp.
    created_at: str = _dataclass_field(default='', metadata={'dex_json_name': 'createdAt'})
    # UpdatedAt is Ashby's update timestamp.
    updated_at: str = _dataclass_field(default='', metadata={'dex_json_name': 'updatedAt'})


setattr(CandidateProfile, '__module__', 'dex_connectors.ashby.models')
