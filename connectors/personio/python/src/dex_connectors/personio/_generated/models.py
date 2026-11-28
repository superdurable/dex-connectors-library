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


# GetPersonInput selects one Personio person by its stable API ID.
@dataclass(frozen=True, slots=True)
class GetPersonInput:
    # PersonID is the ID returned by the Personio v2 Persons API.
    person_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'personId'})


setattr(GetPersonInput, '__module__', 'dex_connectors.personio.models')


# PersonProfile contains selected names and an ID, excluding email and other personnel data.
@dataclass(frozen=True, slots=True)
class PersonProfile:
    # ID is the stable Personio person ID.
    id: str = ''
    # FirstName is the person's first name, or empty when absent.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName'})
    # LastName is the person's last name, or empty when absent.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName'})


setattr(PersonProfile, '__module__', 'dex_connectors.personio.models')


# ListPersonsInput selects a page and optional Personio filters.
@dataclass(frozen=True, slots=True)
class ListPersonsInput:
    # Limit is 1 to 50 persons; zero uses Personio's default page size.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Cursor is the opaque cursor from a previous page; blank starts at the first page.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # FirstName filters exact first names; blank disables the filter.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName', 'dex_json_omit_default': True})
    # LastName filters exact last names; blank disables the filter.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName', 'dex_json_omit_default': True})
    # Status is ACTIVE or INACTIVE; blank includes both.
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListPersonsInput, '__module__', 'dex_connectors.personio.models')


# PersonPage contains selected person fields and the next page cursor.
@dataclass(frozen=True, slots=True)
class PersonPage:
    # Persons contains non-secret ID and name fields only.
    persons: tuple[PersonProfile, ...] = ()
    # NextCursor is blank when no following page is advertised.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'persons', _replace_nil_slices(self.persons, 1))


PersonPage.__annotations__['persons'] = tuple[PersonProfile, ...] | None
PersonPage.__dataclass_fields__['persons'].type = tuple[PersonProfile, ...] | None
setattr(PersonPage, '__module__', 'dex_connectors.personio.models')
