# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetCandidateInput selects a non-secret Greenhouse candidate ID.
@dataclass(frozen=True, slots=True)
class GetCandidateInput:
    # CandidateID is the positive integer ID from a Greenhouse candidate page or API list.
    candidate_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'candidateId'})


setattr(GetCandidateInput, '__module__', 'dex_connectors.greenhouse.models')


# CandidateProfile excludes email addresses, phone numbers, resumes, and notes.
@dataclass(frozen=True, slots=True)
class CandidateProfile:
    # ID is the Greenhouse candidate ID.
    id: int = 0
    # FirstName is the candidate's first name, or empty when Greenhouse stores null.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName'})
    # LastName is the candidate's last name, or empty when Greenhouse stores null.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName'})


setattr(CandidateProfile, '__module__', 'dex_connectors.greenhouse.models')
