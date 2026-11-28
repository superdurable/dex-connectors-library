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


# ListCandidatesInput selects one page of candidate profiles.
@dataclass(frozen=True, slots=True)
class ListCandidatesInput:
    # PageSize is 1 to 500; zero selects Greenhouse's default page size.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Cursor is the opaque value returned by a preceding page; leave blank for the first page.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListCandidatesInput, '__module__', 'dex_connectors.greenhouse.models')


# CandidateProfilesPage contains selected profile fields and an optional next cursor.
@dataclass(frozen=True, slots=True)
class CandidateProfilesPage:
    # Candidates contains only IDs and names, without contact information.
    candidates: tuple[CandidateProfile, ...] = ()
    # NextCursor is blank when the last page has been read.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'candidates', _replace_nil_slices(self.candidates, 1))


CandidateProfilesPage.__annotations__['candidates'] = tuple[CandidateProfile, ...] | None
CandidateProfilesPage.__dataclass_fields__['candidates'].type = tuple[CandidateProfile, ...] | None
setattr(CandidateProfilesPage, '__module__', 'dex_connectors.greenhouse.models')
