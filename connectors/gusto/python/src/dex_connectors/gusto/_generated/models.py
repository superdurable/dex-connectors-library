# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

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
