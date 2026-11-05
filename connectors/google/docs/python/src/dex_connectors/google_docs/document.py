# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Validate and render the first Google Docs tab."""

import json
import re
from typing import Any, cast

from .client import DRIVE_ID, TAB_ID

HEADING_LEVELS = {"TITLE": 1, "SUBTITLE": 2, **{f"HEADING_{level}": level for level in range(1, 7)}}
ORDERED_GLYPHS = {"DECIMAL", "ZERO_DECIMAL", "UPPER_ALPHA", "ALPHA", "UPPER_ROMAN", "ROMAN"}
MARKDOWN_START = re.compile(
    r"^(?:#{1,6}(?:\s|$)|[-+*](?:\s|$)|>|```|~~~|(?:-\s*){3,}$|(?:\*\s*){3,}$|(?:_\s*){3,}$|=+\s*$)"
)
ORDERED_START = re.compile(r"^(\d{1,9})([.)])(\s|$)")


def decode_document(body: bytes) -> dict[str, Any]:
    """Reject a document without a valid ID, first tab, and final paragraph."""
    try:
        document: Any = json.loads(body)
        if (
            not isinstance(document, dict)
            or not isinstance(document.get("documentId"), str)
            or not DRIVE_ID.fullmatch(document["documentId"])
        ):
            raise ValueError("document response lacks a valid document ID")
        tabs = document["tabs"]
        first = tabs[0]
        if (
            not isinstance(first, dict)
            or not isinstance(first.get("documentTab"), dict)
            or not TAB_ID.fullmatch(first["tabProperties"]["tabId"])
        ):
            raise ValueError("document response lacks its first tab")
        content = first["documentTab"]["body"]["content"]
        if (
            not isinstance(content, list)
            or not content
            or not isinstance(content[-1].get("paragraph"), dict)
            or not isinstance(content[-1].get("endIndex"), int)
            or content[-1]["endIndex"] < 2
        ):
            raise ValueError("document body does not end with a paragraph")
        if not isinstance(document.get("title", ""), str) or not isinstance(
            document.get("revisionId", ""), str
        ):
            raise ValueError("document title or revision ID is invalid")
        return document
    except (KeyError, IndexError, TypeError, ValueError) as error:
        raise ValueError("provider returned an invalid document") from error


def first_content(document: dict[str, Any]) -> list[dict[str, Any]]:
    """Return the first tab's structural elements."""
    return cast(list[dict[str, Any]], document["tabs"][0]["documentTab"]["body"]["content"])


def body_text(document: dict[str, Any]) -> str:
    """Join all first-tab text runs; non-text content blocks placeholders."""

    def collect(content: list[dict[str, Any]], depth: int) -> str:
        if depth > 16:
            raise ValueError("document tables are nested too deeply")
        parts: list[str] = []
        for element in content:
            if "paragraph" in element:
                for item in element["paragraph"].get("elements", []):
                    if "textRun" in item:
                        value = item["textRun"].get("content", "")
                        if not isinstance(value, str):
                            raise ValueError("invalid text run")
                        parts.append(value)
                    else:
                        parts.append("\ue907")
            elif "table" in element:
                for row in element["table"].get("tableRows", []):
                    for cell in row.get("tableCells", []):
                        parts.append(collect(cell.get("content", []), depth + 1))
        return "".join(parts)

    return collect(first_content(document), 0)


def _inline_text(paragraph: dict[str, Any], markdown: bool) -> tuple[str, bool]:
    parts: list[str] = []
    has_rule = False
    for element in paragraph.get("elements", []):
        if "textRun" in element:
            parts.append(element["textRun"].get("content", "").replace("\ue907", "").replace("\v", "\n"))
        elif "person" in element:
            properties = element["person"].get("personProperties", {})
            parts.append(properties.get("name") or properties.get("email", ""))
        elif "richLink" in element:
            properties = element["richLink"].get("richLinkProperties", {})
            title, uri = properties.get("title") or properties.get("uri", ""), properties.get("uri", "")
            if markdown and uri:
                safe_title = title.replace("\\", "\\\\").replace("[", "\\[").replace("]", "\\]")
                safe_uri = (
                    uri.replace(" ", "%20")
                    .replace("(", "%28")
                    .replace(")", "%29")
                    .replace("<", "%3C")
                    .replace(">", "%3E")
                )
                parts.append(f"[{safe_title}]({safe_uri})")
            else:
                parts.append(title)
        elif "dateElement" in element:
            parts.append(element["dateElement"].get("dateElementProperties", {}).get("displayText", ""))
        elif "footnoteReference" in element:
            number = element["footnoteReference"].get("footnoteNumber", "")
            parts.append(f"[^{number}]" if markdown else f"[{number}]")
        elif "horizontalRule" in element:
            has_rule = True
    return "".join(parts).removesuffix("\n"), has_rule


def render_document(document: dict[str, Any], format_name: str) -> str:
    """Render paragraphs, lists, and tables using Go's first-tab rules."""
    markdown = format_name == "markdown"
    lists = document["tabs"][0]["documentTab"].get("lists") or {}
    counters: dict[str, list[int]] = {}

    def render(content: list[dict[str, Any]], depth: int) -> list[tuple[str, bool]]:
        if depth > 16:
            raise ValueError("document tables are nested too deeply")
        blocks: list[tuple[str, bool]] = []
        for element in content:
            if "paragraph" in element:
                paragraph = element["paragraph"]
                value, has_rule = _inline_text(paragraph, markdown)
                bullet = paragraph.get("bullet")
                if bullet:
                    level = min(max(bullet.get("nestingLevel", 0), 0), 8)
                    list_id = bullet.get("listId", "")
                    levels = lists.get(list_id, {}).get("listProperties", {}).get("nestingLevels", [])
                    ordered = level < len(levels) and levels[level].get("glyphType") in ORDERED_GLYPHS
                    marker = "- "
                    if ordered:
                        values = counters.setdefault(list_id, [])
                        values.extend([0] * max(level + 1 - len(values), 0))
                        values[level] += 1
                        values[level + 1 :] = [0] * (len(values) - level - 1)
                        marker = f"{values[level]}. "
                    blocks.append(
                        (("    " if markdown else "  ") * level + marker + value.replace("\n", " "), True)
                    )
                    continue
                style = paragraph.get("paragraphStyle", {}).get("namedStyleType", "")
                if style in HEADING_LEVELS and value.strip():
                    value = value.replace("\n", " ")
                    blocks.append((("#" * HEADING_LEVELS[style] + " " if markdown else "") + value, False))
                    continue
                if not value.strip():
                    if has_rule and markdown:
                        blocks.append(("---", False))
                    elif not markdown:
                        blocks.append(("", False))
                    continue
                if markdown:
                    lines = []
                    for line in value.split("\n"):
                        matched = ORDERED_START.match(line)
                        if matched:
                            line = line[: matched.start(2)] + "\\" + line[matched.start(2) :]
                        elif MARKDOWN_START.match(line):
                            line = "\\" + line
                        lines.append(line)
                    value = "\n".join(lines)
                blocks.append((value, False))
            elif "table" in element:
                rows = element["table"].get("tableRows", [])
                rendered_rows: list[list[str]] = []
                for row in rows:
                    cells: list[str] = []
                    for cell in row.get("tableCells", []):
                        cell_parts = [
                            part for part, _ in render(cell.get("content", []), depth + 1) if part.strip()
                        ]
                        cell_value = " ".join(cell_parts).replace("\n", " ")
                        cells.append(
                            cell_value.replace("|", "\\|") if markdown else cell_value.replace("\t", " ")
                        )
                    rendered_rows.append(cells)
                columns = max((len(row) for row in rendered_rows), default=0)
                if columns:
                    table_lines: list[str] = []
                    for index, row in enumerate(rendered_rows):
                        row += [""] * (columns - len(row))
                        table_lines.append("| " + " | ".join(row) + " |" if markdown else "\t".join(row))
                        if markdown and index == 0:
                            table_lines.append("|" + " --- |" * columns)
                    blocks.append(("\n".join(table_lines), False))
        return blocks

    blocks = render(first_content(document), 0)
    result = ""
    for index, (value, is_list) in enumerate(blocks):
        if index:
            separator = "\n" if not markdown or (is_list and blocks[index - 1][1]) else "\n\n"
            result += separator
        result += value
    return result.strip("\n")


def validate_write_text(field: str, value: str) -> None:
    """Reject characters that Google strips or changes on insertion."""
    for character in value:
        code = ord(character)
        if character == "\r":
            raise ValueError(f"{field} must use \\n line breaks without \\r")
        if code <= 8 or 12 <= code <= 31:
            raise ValueError(f"{field} cannot contain control characters other than tab and line breaks")
        if 0xE000 <= code <= 0xF8FF:
            raise ValueError(f"{field} cannot contain private-use characters, which Google strips")


def utf16_length(value: str) -> int:
    """Count Google Docs UTF-16 edit indexes."""
    return len(value.encode("utf-16-le")) // 2
