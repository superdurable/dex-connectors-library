# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListModelsInput has no arguments; the connection identifies the accessible catalog.
@dataclass(frozen=True, slots=True)
class ListModelsInput:
    pass


setattr(ListModelsInput, '__module__', 'dex_connectors.tokengp.models')


# Model identifies a model accessible to the connected TokenGP API key.
@dataclass(frozen=True, slots=True)
class Model:
    # ID is the provider's model identifier.
    id: str = ''


setattr(Model, '__module__', 'dex_connectors.tokengp.models')


# ListModelsOutput contains only accessible model IDs, excluding pricing and account data.
@dataclass(frozen=True, slots=True)
class ListModelsOutput:
    # Models are the provider's accessible models in returned order.
    models: tuple[Model, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'models', _replace_nil_slices(self.models, 1))


ListModelsOutput.__annotations__['models'] = tuple[Model, ...] | None
ListModelsOutput.__dataclass_fields__['models'].type = tuple[Model, ...] | None
setattr(ListModelsOutput, '__module__', 'dex_connectors.tokengp.models')
