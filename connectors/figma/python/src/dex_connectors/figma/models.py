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

GetFileInput.__doc__ = "Figma file key from the file URL, such as figma.com/design/<key>/..."
File.__doc__ = "File name, last-modified timestamp text, and the untouched document node tree."
GetFileCommentsInput.__doc__ = "Figma file key whose comments are read."
FileComment.__doc__ = "One comment's ID and message text."
