# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetDocumentInput selects a document by its non-secret ID.
@dataclass(frozen=True, slots=True)
class GetDocumentInput:
    # DocumentID is the ID returned by PandaDoc when a document is created.
    document_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'documentId'})


setattr(GetDocumentInput, '__module__', 'dex_connectors.pandadoc.models')


# DocumentStatus excludes recipients, fields, tokens, and pricing.
@dataclass(frozen=True, slots=True)
class DocumentStatus:
    # ID is the PandaDoc document ID.
    id: str = ''
    # Name is the document title.
    name: str = ''
    # Status is a PandaDoc document state such as document.completed.
    status: str = ''


setattr(DocumentStatus, '__module__', 'dex_connectors.pandadoc.models')
