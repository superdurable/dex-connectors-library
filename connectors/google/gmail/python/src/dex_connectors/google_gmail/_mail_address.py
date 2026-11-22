# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Mailbox parsing and formatting ported from Go 1.27's net/mail, so Python reads mailboxes as Go does.

Unlike Go, other charsets decode through Python codecs, or stay literal when unknown, as in TypeScript.
Invalid bytes in a utf-8 encoded word become U+FFFD, where Go keeps them, as in TypeScript.
A display name made only of empty encoded words reads as no name, as in Go 1.24 and TypeScript.
A comment may follow an encoded word in a display name, as in Go 1.24 and Python v0.5.1.
"""

import base64
import ipaddress
import re
from dataclasses import dataclass

_ENCODED_WORD = re.compile(r"=\?([^?]+)\?([BbQq])\?([^?]*)\?=")
_PADDED_BASE64 = re.compile(r"(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?")
_Q_ENCODED_BYTE = re.compile(r"[0-9A-Fa-f]{2}")
_WHITESPACE_RUN = re.compile(r"[ \t]+")
# RFC 5322 3.2.3 specials, which Go's isAtext excludes along with the period outside a dot-atom.
_ATOM_SPECIALS = frozenset('()<>[]:;@\\,"')
# Go's mail.Address.String keeps printable ASCII, space, and tab in a quoted string.
_QUOTABLE_DISPLAY_NAME = re.compile(r"[\t\x20-\x7e]*")
# RFC 2047 5(3) forbids these in a Q-encoded phrase, so Go B-encodes; Go misses the backslash.
_B_ENCODED_DISPLAY_NAME_CHARACTER = re.compile(r"[\"#$%&'(),.:;<>@\[\\\]^`{|}~]")
# An encoded word has at most 75 characters, 63 of them between "=?utf-8?q?" and "?=".
_MAX_ENCODED_WORD_TEXT_LENGTH = 63
# 45 bytes fill 60 Base64 characters, the most that fit in 63.
_MAX_B_ENCODED_WORD_BYTES = 45


@dataclass(frozen=True)
class MailAddress:
    """One mailbox as Go's mail.Address holds it: a decoded display name and an unquoted addr-spec."""

    name: str
    address: str


class MailAddressSyntaxError(ValueError):
    """The text is not a mailbox or address list that Go's net/mail accepts."""


def parse_mail_address(value: str) -> MailAddress:
    """Parse one mailbox as Go's mail.ParseAddress does.

    Raises:
        MailAddressSyntaxError: when Go rejects the mailbox.
    """
    return _MailAddressParser(value).parse_single_address()


def parse_mail_address_list(value: str) -> list[MailAddress]:
    """Parse an address list, groups flattened, as Go's mail.ParseAddressList does.

    Raises:
        MailAddressSyntaxError: when Go rejects any part of the list.
    """
    return _MailAddressParser(value).parse_address_list()


def format_mail_address(mailbox: MailAddress) -> str:
    """Format a mailbox as the Go connector does.

    That is Go's mail.Address.String, except that a non-ASCII name with a backslash is B-encoded.
    """
    angle_address = "<" + _format_addr_spec(mailbox.address) + ">"
    if not mailbox.name:
        return angle_address
    return f"{_format_display_name(mailbox.name)} {angle_address}"


class _MailAddressParser:
    """Go's net/mail addrParser, reading by index so a long header parses in linear time."""

    def __init__(self, text: str) -> None:
        self._text = text
        self._position = 0

    def parse_address_list(self) -> list[MailAddress]:
        addresses: list[MailAddress] = []
        while True:
            self._skip_space()
            # Go skips empty list entries (RFC 5322 obs-addr-list).
            if self._consume(","):
                continue
            addresses.extend(self._parse_address(is_group_allowed=True))
            self._skip_required_cfws()
            if self._is_empty():
                return addresses
            if self._peek() != ",":
                raise MailAddressSyntaxError("expected comma")
            while self._consume(","):
                self._skip_space()
            if self._is_empty():
                return addresses

    def parse_single_address(self) -> MailAddress:
        addresses = self._parse_address(is_group_allowed=True)
        self._skip_required_cfws()
        if not self._is_empty():
            raise MailAddressSyntaxError("expected single address")
        if len(addresses) != 1:
            raise MailAddressSyntaxError("group is empty or has several addresses")
        return addresses[0]

    def _parse_address(self, is_group_allowed: bool) -> list[MailAddress]:
        self._skip_space()
        if self._is_empty():
            raise MailAddressSyntaxError("no address")
        # Go tries the stricter addr-spec first; a comment after a bare one is its display name.
        address = self._consume_addr_spec()
        if address is not None:
            self._skip_space()
            name = self._consume_display_name_comment() if self._consume("(") else ""
            return [MailAddress(name, address)]
        name = "" if self._peek() == "<" else self._consume_phrase()
        self._skip_space()
        if is_group_allowed and self._consume(":"):
            return self._consume_group_list()
        if not self._consume("<"):
            raise MailAddressSyntaxError("no angle-addr")
        address = self._consume_addr_spec()
        if address is None:
            raise MailAddressSyntaxError("invalid addr-spec")
        if not self._consume(">"):
            raise MailAddressSyntaxError("unclosed angle-addr")
        return [MailAddress(name, address)]

    def _consume_group_list(self) -> list[MailAddress]:
        group: list[MailAddress] = []
        self._skip_space()
        if self._consume(";"):
            self._skip_required_cfws()
            return group
        while True:
            self._skip_space()
            group.extend(self._parse_address(is_group_allowed=False))
            self._skip_required_cfws()
            if self._consume(";"):
                self._skip_required_cfws()
                return group
            if not self._consume(","):
                raise MailAddressSyntaxError("expected comma")

    def _consume_addr_spec(self) -> str | None:
        # Go restores the input when no addr-spec parses here.
        start = self._position
        try:
            self._skip_space()
            if self._is_empty():
                raise MailAddressSyntaxError("no addr-spec")
            if self._peek() == '"':
                local_part = self._consume_quoted_string()
                if not local_part:
                    raise MailAddressSyntaxError("empty quoted string in addr-spec")
            else:
                local_part = self._consume_atom(is_dot_allowed=True, is_permissive=False)
            if not self._consume("@"):
                raise MailAddressSyntaxError("missing @ in addr-spec")
            self._skip_space()
            if self._is_empty():
                raise MailAddressSyntaxError("no domain in addr-spec")
            if self._peek() == "[":
                domain = self._consume_domain_literal()
            else:
                domain = self._consume_atom(is_dot_allowed=True, is_permissive=False)
        except MailAddressSyntaxError:
            self._position = start
            return None
        return f"{local_part}@{domain}"

    def _consume_phrase(self) -> str:
        # Like Go 1.26 and later, adjacent encoded words join and an empty one adds no word.
        words: list[str] = []
        encoded_run = ""
        # Like Go 1.24, any word read, encoded or empty, allows CFWS after it and an empty name.
        is_word_read = False
        is_word_missing = False
        while True:
            # Go allows CFWS between phrase words (obs-phrase).
            if is_word_read:
                self._skip_required_cfws()
            self._skip_space()
            if self._is_empty():
                break
            try:
                word, is_encoded = self._consume_phrase_word()
            except MailAddressSyntaxError:
                is_word_missing = True
                break
            is_word_read = True
            if is_encoded:
                encoded_run += word
                continue
            if encoded_run:
                words.append(encoded_run)
                encoded_run = ""
            words.append(word)
        if encoded_run:
            words.append(encoded_run)
        if is_word_missing and not is_word_read:
            raise MailAddressSyntaxError("missing word in phrase")
        return " ".join(words)

    def _consume_phrase_word(self) -> tuple[str, bool]:
        if self._peek() == '"':
            return self._consume_quoted_string(), False
        # Go reads a permissive dot-atom here.
        atom = self._consume_atom(is_dot_allowed=True, is_permissive=True)
        decoded = _decode_encoded_word(atom)
        return (atom, False) if decoded is None else (decoded, True)

    def _consume_quoted_string(self) -> str:
        # The opening quote is at the current position; nothing is consumed on error.
        index = self._position + 1
        characters: list[str] = []
        is_escaped = False
        while True:
            if index >= len(self._text):
                raise MailAddressSyntaxError("unclosed quoted-string")
            character = self._text[index]
            if is_escaped:
                if not _is_vchar(character) and not _is_wsp(character):
                    raise MailAddressSyntaxError("bad character in quoted-string")
                characters.append(character)
                is_escaped = False
            elif _is_qtext(character) or _is_wsp(character):
                characters.append(character)
            elif character == '"':
                break
            elif character == "\\":
                is_escaped = True
            else:
                raise MailAddressSyntaxError("bad character in quoted-string")
            index += 1
        self._position = index + 1
        return "".join(characters)

    def _consume_atom(self, is_dot_allowed: bool, is_permissive: bool) -> str:
        end = self._position
        while end < len(self._text) and _is_atext(self._text[end], is_dot_allowed):
            end += 1
        if end == self._position:
            raise MailAddressSyntaxError("invalid string")
        atom = self._text[self._position:end]
        self._position = end
        if not is_permissive and (atom.startswith(".") or atom.endswith(".") or ".." in atom):
            raise MailAddressSyntaxError("leading, trailing, or double dot in atom")
        return atom

    def _consume_domain_literal(self) -> str:
        # The opening bracket is at the current position.
        start = self._position + 1
        end = start
        while end < len(self._text) and self._text[end] != "]":
            if not _is_dtext(self._text[end]):
                raise MailAddressSyntaxError("bad character in domain-literal")
            end += 1
        if end >= len(self._text):
            raise MailAddressSyntaxError("unclosed domain-literal")
        self._position = end + 1
        text = self._text[start:end]
        if not _is_go_domain_literal_address(text):
            raise MailAddressSyntaxError("invalid IP address in domain-literal")
        return f"[{text}]"

    def _consume_display_name_comment(self) -> str:
        comment = self._consume_comment()
        if comment is None:
            raise MailAddressSyntaxError("misformatted parenthetical comment")
        # Go decodes each word alone here, so adjacent encoded words keep their space.
        words = [word for word in _WHITESPACE_RUN.split(comment) if word]
        return " ".join(_decode_comment_word(word) for word in words)

    def _consume_comment(self) -> str | None:
        # The opening parenthesis is consumed; a backslash keeps the next character literally.
        depth = 1
        characters: list[str] = []
        while not self._is_empty() and depth > 0:
            character = self._peek()
            if character == "\\" and self._position + 1 < len(self._text):
                self._position += 1
            elif character == "(":
                depth += 1
            elif character == ")":
                depth -= 1
            if depth > 0:
                characters.append(self._peek())
            self._position += 1
        return "".join(characters) if depth == 0 else None

    def _skip_required_cfws(self) -> None:
        self._skip_space()
        while self._consume("("):
            if self._consume_comment() is None:
                raise MailAddressSyntaxError("misformatted parenthetical comment")
            self._skip_space()

    def _skip_space(self) -> None:
        while self._position < len(self._text) and self._text[self._position] in " \t":
            self._position += 1

    def _consume(self, character: str) -> bool:
        if self._is_empty() or self._peek() != character:
            return False
        self._position += 1
        return True

    def _peek(self) -> str:
        return self._text[self._position]

    def _is_empty(self) -> bool:
        return self._position >= len(self._text)


def _is_vchar(character: str) -> bool:
    # RFC 6532 admits every non-ASCII character, as Go's isMultibyte does.
    return "!" <= character <= "~" or character >= "\x80"


def _is_wsp(character: str) -> bool:
    return character in (" ", "\t")


def _is_atext(character: str, is_dot_allowed: bool) -> bool:
    if character == ".":
        return is_dot_allowed
    return character not in _ATOM_SPECIALS and _is_vchar(character)


def _is_qtext(character: str) -> bool:
    return character not in ('"', "\\") and _is_vchar(character)


def _is_dtext(character: str) -> bool:
    return character not in ("[", "]", "\\") and _is_vchar(character)


def _is_go_domain_literal_address(text: str) -> bool:
    # Go 1.27: "IPv6:" tags any IP address; an untagged one must be IPv4 or IPv4-mapped.
    is_tagged = text.startswith("IPv6:")
    # Go's net.ParseIP takes no IPv6 zone.
    if "%" in text:
        return False
    try:
        address = ipaddress.ip_address(text.removeprefix("IPv6:"))
    except ValueError:
        return False
    return is_tagged or isinstance(address, ipaddress.IPv4Address) or address.ipv4_mapped is not None


def _decode_comment_word(word: str) -> str:
    decoded = _decode_encoded_word(word)
    return word if decoded is None else decoded


def _decode_encoded_word(word: str) -> str | None:
    # Go's mime.WordDecoder rules; None leaves the word literal.
    match = _ENCODED_WORD.fullmatch(word)
    if match is None:
        return None
    charset, encoding, text = match.groups()
    content = _decode_b_encoded_text(text) if encoding in "Bb" else _decode_q_encoded_text(text)
    return None if content is None else _decode_charset_bytes(charset, content)


def _decode_b_encoded_text(text: str) -> bytes | None:
    return base64.b64decode(text) if _PADDED_BASE64.fullmatch(text) is not None else None


def _decode_q_encoded_text(text: str) -> bytes | None:
    content = bytearray()
    index = 0
    while index < len(text):
        character = text[index]
        if character == "_":
            content.append(0x20)
        elif character == "=":
            hex_digits = text[index + 1:index + 3]
            if _Q_ENCODED_BYTE.fullmatch(hex_digits) is None:
                return None
            content.append(int(hex_digits, 16))
            index += 2
        elif " " <= character <= "~":
            content.append(ord(character))
        else:
            return None
        index += 1
    return bytes(content)


def _decode_charset_bytes(charset: str, content: bytes) -> str | None:
    match charset.lower():
        case "utf-8":
            return content.decode("utf-8", errors="replace")
        case "iso-8859-1":
            return content.decode("latin-1")
        case "us-ascii":
            return "".join(chr(byte) if byte < 0x80 else "�" for byte in content)
    # Go fails other charsets; a known codec decodes them, as TypeScript's TextDecoder does.
    try:
        return content.decode(charset)
    except (LookupError, ValueError):
        # An unknown charset, invalid bytes, or a codec such as idna that raises UnicodeError.
        return None


def _format_addr_spec(address: str) -> str:
    # Go's mail.Address.String quotes a local part that is not a dot-atom.
    local_part, _, domain = address.rpartition("@")
    for index, character in enumerate(local_part):
        if _is_atext(character, is_dot_allowed=False):
            continue
        if character == "." and 0 < index < len(local_part) - 1 and local_part[index - 1] != ".":
            continue
        return f"{_quote_string(local_part)}@{domain}"
    return address


def _quote_string(text: str) -> str:
    # Go's quoteString escapes quotes and backslashes and drops control characters.
    quoted = "".join(
        character if _is_qtext(character) or _is_wsp(character) else "\\" + character
        for character in text
        if _is_vchar(character) or _is_wsp(character)
    )
    return f'"{quoted}"'


def _format_display_name(display_name: str) -> str:
    # A lone surrogate becomes U+FFFD, as Go's JSON decoder and the TypeScript UTF-8 encoder make it.
    display_name = display_name.encode("utf-16", "surrogatepass").decode("utf-16", "replace")
    if _QUOTABLE_DISPLAY_NAME.fullmatch(display_name) is not None:
        return _quote_string(display_name)
    if _B_ENCODED_DISPLAY_NAME_CHARACTER.search(display_name) is not None:
        return _encode_b_words(display_name)
    return _encode_q_words(display_name)


def _encode_q_words(display_name: str) -> str:
    # Go's mime.QEncoding splitting: never inside a character, and only where the next one would pass 63.
    words: list[str] = []
    word = ""
    for character in display_name:
        if " " <= character <= "~" and character not in "=?_":
            encoded = "_" if character == " " else character
        else:
            encoded = "".join(f"={byte:02X}" for byte in character.encode("utf-8"))
        if len(word) + len(encoded) > _MAX_ENCODED_WORD_TEXT_LENGTH:
            words.append(word)
            word = ""
        word += encoded
    words.append(word)
    return " ".join(f"=?utf-8?q?{text}?=" for text in words)


def _encode_b_words(display_name: str) -> str:
    # Go's mime.BEncoding splitting: whole characters, at most 45 bytes per word.
    words: list[bytes] = [b""]
    for character in display_name:
        encoded = character.encode("utf-8")
        if len(words[-1]) + len(encoded) > _MAX_B_ENCODED_WORD_BYTES:
            words.append(b"")
        words[-1] += encoded
    return " ".join(f"=?utf-8?b?{base64.b64encode(word).decode()}?=" for word in words)
