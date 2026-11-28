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


# GetEmployeeInput identifies one employee in the authorized company.
@dataclass(frozen=True, slots=True)
class GetEmployeeInput:
    # EmployeeID is the employee UUID from Gusto's employee list or webhook.
    employee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'employeeId'})


setattr(GetEmployeeInput, '__module__', 'dex_connectors.gusto.models')


# Employee projects safe identity and contact fields from one Gusto employee.
@dataclass(frozen=True, slots=True)
class Employee:
    # EmployeeID is the stable Gusto employee UUID.
    employee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'employeeId'})
    # FirstName is the employee's given name.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName'})
    # LastName is the employee's family name.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName'})
    # Email is the personal email when Gusto supplies one; blank means absent.
    email: str = ''
    # CompanyID is the employing company's UUID.
    company_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'companyId'})


setattr(Employee, '__module__', 'dex_connectors.gusto.models')


# ListEmployeesInput selects one bounded page from an authorized company.
@dataclass(frozen=True, slots=True)
class ListEmployeesInput:
    # CompanyID is the company UUID from Gusto's authorized company.
    company_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'companyId'})
    # Page is a positive one-based page number.
    page: int = 0
    # PerPage is the number of employees per page, from 1 to 100.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage'})


setattr(ListEmployeesInput, '__module__', 'dex_connectors.gusto.models')


# EmployeesPage contains selected employee fields for one company page.
@dataclass(frozen=True, slots=True)
class EmployeesPage:
    # Employees excludes compensation, tax, address, and job details.
    employees: tuple[Employee, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'employees', _replace_nil_slices(self.employees, 1))


EmployeesPage.__annotations__['employees'] = tuple[Employee, ...] | None
EmployeesPage.__dataclass_fields__['employees'].type = tuple[Employee, ...] | None
setattr(EmployeesPage, '__module__', 'dex_connectors.gusto.models')
