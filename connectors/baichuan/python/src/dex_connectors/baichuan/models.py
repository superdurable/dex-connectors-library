# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public Baichuan models generated from the Go connector contract."""

from ._generated.models import KnowledgeBase as KnowledgeBase
from ._generated.models import ListKnowledgeBasesInput as ListKnowledgeBasesInput
from ._generated.models import ListKnowledgeBasesOutput as ListKnowledgeBasesOutput

KnowledgeBase.__doc__ = "One knowledge base's provider ID, metadata, and attached file IDs."
ListKnowledgeBasesInput.__doc__ = "Request the complete Baichuan knowledge-base list."
ListKnowledgeBasesOutput.__doc__ = "All knowledge bases visible to the configured API key."
