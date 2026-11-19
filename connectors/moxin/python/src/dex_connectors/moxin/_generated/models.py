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


# ListModelsInput requires no selector; the token and group define visibility.
@dataclass(frozen=True, slots=True)
class ListModelsInput:
    pass


setattr(ListModelsInput, '__module__', 'dex_connectors.moxin.models')


# Model identifies a model currently available to the configured token.
@dataclass(frozen=True, slots=True)
class Model:
    # ID is the model identifier accepted by Moxin generation APIs.
    id: str = ''


setattr(Model, '__module__', 'dex_connectors.moxin.models')


# ModelPage contains the current authorized model IDs.
@dataclass(frozen=True, slots=True)
class ModelPage:
    # Models may be empty when no model is assigned to the token's group.
    models: tuple[Model, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'models', _replace_nil_slices(self.models, 1))


ModelPage.__annotations__['models'] = tuple[Model, ...] | None
ModelPage.__dataclass_fields__['models'].type = tuple[Model, ...] | None
setattr(ModelPage, '__module__', 'dex_connectors.moxin.models')
