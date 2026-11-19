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


# ListModelsInput has no caller-supplied filters.
@dataclass(frozen=True, slots=True)
class ListModelsInput:
    pass


setattr(ListModelsInput, '__module__', 'dex_connectors.infiniai.models')


# ModelInfo contains the documented model identifier, type, and token limits.
@dataclass(frozen=True, slots=True)
class ModelInfo:
    # ID is the model identifier used in GenStudio API calls.
    id: str = ''
    # ModelType is the provider's model category.
    model_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'modelType'})
    # ContextLength is the maximum context token count; zero means unavailable or inapplicable.
    context_length: int = _dataclass_field(default=0, metadata={'dex_json_name': 'contextLength'})
    # MaxOutputLength is the maximum output token count; zero means unavailable or inapplicable.
    max_output_length: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxOutputLength'})


setattr(ModelInfo, '__module__', 'dex_connectors.infiniai.models')


# ListModelsOutput contains all models currently available to the tenant.
@dataclass(frozen=True, slots=True)
class ListModelsOutput:
    # Models is ordered as returned by GenStudio.
    models: tuple[ModelInfo, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'models', _replace_nil_slices(self.models, 1))


ListModelsOutput.__annotations__['models'] = tuple[ModelInfo, ...] | None
ListModelsOutput.__dataclass_fields__['models'].type = tuple[ModelInfo, ...] | None
setattr(ListModelsOutput, '__module__', 'dex_connectors.infiniai.models')
