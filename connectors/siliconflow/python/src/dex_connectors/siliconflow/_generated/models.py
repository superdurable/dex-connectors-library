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


# ListModelsInput requests the catalog available to the connection's API key.
@dataclass(frozen=True, slots=True)
class ListModelsInput:
    pass


setattr(ListModelsInput, '__module__', 'dex_connectors.siliconflow.models')


# Model is one SiliconFlow catalog entry.
@dataclass(frozen=True, slots=True)
class Model:
    # ID is the exact model name to pass to a SiliconFlow inference request.
    id: str = ''
    # Object is the provider's object discriminator, normally "model".
    object: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(Model, '__module__', 'dex_connectors.siliconflow.models')


# ListModelsOutput contains the current model catalog for the API key.
@dataclass(frozen=True, slots=True)
class ListModelsOutput:
    # Models contains zero or more entries and does not imply inference permission for each model.
    models: tuple[Model, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'models', _replace_nil_slices(self.models, 1))


ListModelsOutput.__annotations__['models'] = tuple[Model, ...] | None
ListModelsOutput.__dataclass_fields__['models'].type = tuple[Model, ...] | None
setattr(ListModelsOutput, '__module__', 'dex_connectors.siliconflow.models')
