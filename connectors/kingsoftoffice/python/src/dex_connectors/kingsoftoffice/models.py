# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public WPS file metadata models generated from the Go contract."""

from ._generated.models import FileMetadata as FileMetadata
from ._generated.models import ListFolderFilesInput as ListFolderFilesInput
from ._generated.models import ListFolderFilesOutput as ListFolderFilesOutput

FileMetadata.__doc__ = "File identifier, name, type, and size without content or sharing data."
ListFolderFilesInput.__doc__ = "Drive and folder identifiers plus an optional page token."
ListFolderFilesOutput.__doc__ = "One bounded page of WPS file metadata."
