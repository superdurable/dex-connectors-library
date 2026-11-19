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


# ListModelsInput has no input because the API Key determines visible models.
@dataclass(frozen=True, slots=True)
class ListModelsInput:
    pass


setattr(ListModelsInput, '__module__', 'dex_connectors.flurouter.models')


# ListModelsOutput contains only model IDs visible to the connected Key.
@dataclass(frozen=True, slots=True)
class ListModelsOutput:
    # ModelIDs are the model identifiers to use in later inference requests.
    model_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'modelIDs'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'model_ids', _replace_nil_slices(self.model_ids, 1))


ListModelsOutput.__annotations__['model_ids'] = tuple[str, ...] | None
ListModelsOutput.__dataclass_fields__['model_ids'].type = tuple[str, ...] | None
setattr(ListModelsOutput, '__module__', 'dex_connectors.flurouter.models')
