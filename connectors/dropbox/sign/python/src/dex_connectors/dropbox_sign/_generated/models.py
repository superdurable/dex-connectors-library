# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetSignatureRequestInput selects a signature request by its non-secret ID.
@dataclass(frozen=True, slots=True)
class GetSignatureRequestInput:
    # SignatureRequestID is the identifier returned when Dropbox Sign creates the request.
    signature_request_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'signatureRequestId'})


setattr(GetSignatureRequestInput, '__module__', 'dex_connectors.dropbox_sign.models')


# SignatureRequestStatus excludes signer identities, document contents, and custom fields.
@dataclass(frozen=True, slots=True)
class SignatureRequestStatus:
    # ID is the Dropbox Sign signature-request ID.
    id: str = ''
    # Title is the signature-request title.
    title: str = ''
    # IsComplete reports whether all required signatures are complete.
    is_complete: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isComplete'})
    # IsDeclined reports whether a signer declined the request.
    is_declined: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDeclined'})
    # HasError reports a provider-side request error.
    has_error: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasError'})


setattr(SignatureRequestStatus, '__module__', 'dex_connectors.dropbox_sign.models')
