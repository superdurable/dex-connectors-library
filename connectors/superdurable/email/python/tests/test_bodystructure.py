# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""IMAP BODYSTRUCTURE supplies complete MIME metadata without reading large bodies."""

import pytest

from dex_connectors.superdurable_email import MessageReference
from dex_connectors.superdurable_email.imap_bodystructure import parse_bodystructure
from dex_connectors.superdurable_email.operations import _decode_text_part, _parse_message


def test_nested_multipart_paths_and_rfc2231_filename() -> None:
    """A nested attachment keeps its IMAP path, encoded size, and decoded filename."""
    structure = (
        b'BODYSTRUCTURE (("TEXT" "PLAIN" ("CHARSET" "UTF-8") NIL NIL "7BIT" 12 1 NIL NIL NIL NIL) '
        b'(("TEXT" "HTML" ("CHARSET" "UTF-8") NIL NIL "7BIT" 40 2 NIL NIL NIL NIL) '
        b'("APPLICATION" "PDF" ("NAME*" "utf-8\'\'facture%20%C3%A9t%C3%A9.pdf") NIL NIL '
        b'"BASE64" 2048 NIL ("ATTACHMENT" ("FILENAME*" "utf-8\'\'facture%20%C3%A9t%C3%A9.pdf")) '
        b'NIL NIL) "RELATED" ("BOUNDARY" "inside") NIL NIL NIL) "MIXED" '
        b'("BOUNDARY" "outside") NIL NIL NIL)'
    )
    parts = parse_bodystructure(structure)
    assert [part.path for part in parts] == [(1,), (2, 1), (2, 2)]
    assert parts[-1].filename == "facture été.pdf"
    assert parts[-1].encoded_size == 2048
    assert parts[-1].is_attachment


def test_continued_rfc2231_filename_segments() -> None:
    """Segmented RFC 2231 names are assembled before decoding."""
    structure = (
        b'BODYSTRUCTURE ("APPLICATION" "PDF" NIL NIL NIL "BASE64" 1024 NIL '
        b'("ATTACHMENT" ("FILENAME*0*" "utf-8\'\'monthly%20" '
        b'"FILENAME*1*" "statement%20%C3%A9.pdf")) NIL NIL)'
    )
    parts = parse_bodystructure(structure)
    assert parts[0].filename == "monthly statement é.pdf"


def test_large_multipart_returns_complete_attachment_metadata() -> None:
    """A 4 MiB message returns BODYSTRUCTURE metadata without fetching its content."""
    metadata = (
        b'1 FETCH (UID 97 FLAGS () INTERNALDATE "03-Nov-2026 18:01:00 +0000" '
        b'RFC822.SIZE 4194304 BODYSTRUCTURE (("TEXT" "PLAIN" ("CHARSET" "UTF-8") '
        b'NIL NIL "7BIT" 16 1 NIL NIL NIL NIL) '
        b'("APPLICATION" "PDF" ("NAME" "invoice.pdf") NIL NIL "BASE64" 4000000 NIL '
        b'("ATTACHMENT" ("FILENAME" "invoice.pdf")) NIL NIL) '
        b'"MIXED" ("BOUNDARY" "outside") NIL NIL NIL))'
    )
    headers = b"From: Jane <jane@example.com>\r\nSubject: Invoice\r\n\r\n"
    message = _parse_message(MessageReference("INBOX", 42, 97), metadata, headers, False)
    assert message.has_attachments
    assert message.attachments[0].part == "2"
    assert message.attachments[0].encoded_size_bytes == 4000000
    assert message.attachments[0].file_name == "invoice.pdf"
    assert message.size_bytes == 4194304


def test_malformed_bodystructure_is_rejected() -> None:
    """A truncated server expression fails instead of producing incomplete metadata."""
    with pytest.raises(ValueError):
        parse_bodystructure(b'BODYSTRUCTURE (("TEXT" "PLAIN"')


def test_unknown_filename_charset_falls_back_to_utf8() -> None:
    """A server-supplied unknown charset does not escape the operation."""
    parts = parse_bodystructure(
        b'BODYSTRUCTURE ("APPLICATION" "PDF" NIL NIL NIL "BASE64" 20 NIL '
        b'("ATTACHMENT" ("FILENAME*" "x-unknown\'\'R%C3%A9sum%C3%A9.pdf")) NIL NIL)'
    )
    assert parts[0].filename == "Résumé.pdf"


def test_plain_filename_continuation_keeps_literal_percent() -> None:
    """Only RFC 2231 starred segments decode percent escapes."""
    parts = parse_bodystructure(
        b'BODYSTRUCTURE ("APPLICATION" "PDF" NIL NIL NIL "BASE64" 20 NIL '
        b'("ATTACHMENT" ("FILENAME*0" "report 100%" "FILENAME*1" "AB.pdf")) NIL NIL)'
    )
    assert parts[0].filename == "report 100%AB.pdf"


def test_partial_base64_text_decodes_complete_quanta() -> None:
    """A bounded text read ends before an incomplete final base64 quantum."""
    part = parse_bodystructure(
        b'BODYSTRUCTURE ("TEXT" "PLAIN" ("CHARSET" "UTF-8") NIL NIL "BASE64" 9 1)'
    )[0]
    text, is_truncated = _decode_text_part(b"SGVsbG8", part)
    assert text == "Hel"
    assert is_truncated
