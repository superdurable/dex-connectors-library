# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    File as File,
)
from ._generated.models import (
    FileComment as FileComment,
)
from ._generated.models import FileComments as FileComments
from ._generated.models import (
    GetFileCommentsInput as GetFileCommentsInput,
)
from ._generated.models import (
    GetFileInput as GetFileInput,
)

FileComments.__doc__ = "All comments Figma returned for the file."
