# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

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
