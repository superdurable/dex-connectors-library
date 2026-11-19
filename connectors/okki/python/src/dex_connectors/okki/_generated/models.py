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


# ListCustomerFieldsInput selects the fixed OKKI company field dictionary.
@dataclass(frozen=True, slots=True)
class ListCustomerFieldsInput:
    pass


setattr(ListCustomerFieldsInput, '__module__', 'dex_connectors.okki.models')


# CustomerField is one field definition without default values or customer data.
@dataclass(frozen=True, slots=True)
class CustomerField:
    # ID is the tenant's stable field identifier.
    id: str = ''
    # Name is the tenant's field label.
    name: str = ''
    # FieldType is OKKI's documented numeric field type.
    field_type: int = _dataclass_field(default=0, metadata={'dex_json_name': 'fieldType'})


setattr(CustomerField, '__module__', 'dex_connectors.okki.models')


# CustomerFields contains a bounded set of customer field definitions.
@dataclass(frozen=True, slots=True)
class CustomerFields:
    # Fields are returned in provider order.
    fields: tuple[CustomerField, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'fields', _replace_nil_slices(self.fields, 1))


CustomerFields.__annotations__['fields'] = tuple[CustomerField, ...] | None
CustomerFields.__dataclass_fields__['fields'].type = tuple[CustomerField, ...] | None
setattr(CustomerFields, '__module__', 'dex_connectors.okki.models')
