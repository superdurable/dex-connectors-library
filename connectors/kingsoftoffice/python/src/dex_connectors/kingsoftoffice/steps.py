# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public operation-specific WPS Step factory."""

from ._generated.steps import new_list_folder_files_step as new_list_folder_files_step

new_list_folder_files_step.__doc__ = "Create the WPS Query Step; unwired optional branches fail the Flow."
