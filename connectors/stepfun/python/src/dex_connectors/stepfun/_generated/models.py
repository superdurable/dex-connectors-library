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


# ListModelsInput has no user-supplied query parameters.
@dataclass(frozen=True, slots=True)
class ListModelsInput:
    pass


setattr(ListModelsInput, '__module__', 'dex_connectors.stepfun.models')


# Model contains the non-secret fields documented for a StepFun model.
@dataclass(frozen=True, slots=True)
class Model:
    # ID is the unique model identifier.
    id: str = ''
    # CreatedAtUnix is the model creation time in Unix seconds.
    created_at_unix: int = _dataclass_field(default=0, metadata={'dex_json_name': 'createdAtUnix'})
    # OwnedBy names the model's organization.
    owned_by: str = _dataclass_field(default='', metadata={'dex_json_name': 'ownedBy'})


setattr(Model, '__module__', 'dex_connectors.stepfun.models')


# ListModelsOutput contains the available model list.
@dataclass(frozen=True, slots=True)
class ListModelsOutput:
    # Models is the available model set; an empty set is valid.
    models: tuple[Model, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'models', _replace_nil_slices(self.models, 1))


ListModelsOutput.__annotations__['models'] = tuple[Model, ...] | None
ListModelsOutput.__dataclass_fields__['models'].type = tuple[Model, ...] | None
setattr(ListModelsOutput, '__module__', 'dex_connectors.stepfun.models')
