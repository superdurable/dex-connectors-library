# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Confluence storage, Markdown, plain text, and ADF conversion."""

from __future__ import annotations

import html
import json
import re
from dataclasses import dataclass, field
from datetime import UTC, datetime
from html.parser import HTMLParser
from urllib.parse import urlsplit

_HEADING = re.compile(r"^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$")
_FENCE = re.compile(r"^ {0,3}(`{3,}|~{3,})[ \t]*([^`]*)$")
_LIST = re.compile(r"^ {0,3}([-*+]|[0-9]{1,9}[.)])(?:[ \t]+|$)(.*)$")
_RULE = re.compile(r"^ {0,3}(?:(?:\*[ \t]*){3,}|(?:-[ \t]*){3,}|(?:_[ \t]*){3,})$")
_TABLE_SEPARATOR = re.compile(
    r"^\|?[ \t]*:?-+:?[ \t]*(?:\|[ \t]*:?-+:?[ \t]*)*\|?$|^\|[ \t]*:?-+:?[ \t]*(?:\|[ \t]*:?-+:?[ \t]*)*\|$"
)
_LINK = re.compile(r"\[([^\]]+)\]\(([^\s)]+)\)")
_BLOCK = frozenset(
    (
        "p",
        "h1",
        "h2",
        "h3",
        "h4",
        "h5",
        "h6",
        "ul",
        "ol",
        "ac:task-list",
        "blockquote",
        "pre",
        "hr",
        "table",
        "div",
        "section",
        "ac:rich-text-body",
        "ac:structured-macro",
    )
)


def _escape_text(value: str) -> str:
    return html.escape(value, quote=False)


def _escape_attribute(value: str) -> str:
    return html.escape(value, quote=True).replace("&#x27;", "&#39;")


def _safe_link(value: str) -> str:
    value = value.strip()
    if len(value) > 2048 or re.search(r"[\s<>\"]", value):
        return ""
    parsed = urlsplit(value)
    return value if parsed.scheme.lower() in ("http", "https", "mailto") else ""


def _inline_storage(value: str, depth: int = 0) -> str:
    if depth > 32:
        return _escape_text(value)
    output: list[str] = []
    index = 0
    while index < len(value):
        if value[index] == "\\" and index + 1 < len(value):
            output.append(_escape_text(value[index + 1]))
            index += 2
            continue
        if value[index] == "\n":
            output.append("<br />")
            index += 1
            continue
        match = _LINK.match(value, index)
        if match:
            href = _safe_link(match.group(2))
            label = _inline_storage(match.group(1), depth + 1)
            output.append(f'<a href="{_escape_attribute(href)}">{label}</a>' if href else label)
            index = match.end()
            continue
        matched = False
        for delimiter, opening, closing in (
            ("**", "<strong>", "</strong>"),
            ("__", "<strong>", "</strong>"),
            ("~~", '<span style="text-decoration: line-through;">', "</span>"),
            ("*", "<em>", "</em>"),
            ("_", "<em>", "</em>"),
            ("`", "<code>", "</code>"),
        ):
            if not value.startswith(delimiter, index):
                continue
            if delimiter == "_" and index and value[index - 1].isalnum():
                continue
            end = value.find(delimiter, index + len(delimiter))
            if end <= index + len(delimiter) or value[index + len(delimiter)].isspace():
                continue
            inner = value[index + len(delimiter) : end]
            if inner[-1].isspace() or (
                delimiter == "_" and end + 1 < len(value) and value[end + 1].isalnum()
            ):
                continue
            contents = (
                _escape_text(inner.replace("\n", " "))
                if delimiter == "`"
                else _inline_storage(inner, depth + 1)
            )
            output.append(opening + contents + closing)
            index = end + len(delimiter)
            matched = True
            break
        if matched:
            continue
        output.append(_escape_text(value[index]))
        index += 1
    return "".join(output)


def _paragraph(text: str, is_plain: bool) -> str:
    lines = [line.strip() for line in text.split("\n")]
    contents = "<br />".join(_escape_text(line) if is_plain else _inline_storage(line) for line in lines)
    return f"<p>{contents}</p>" if contents.strip() else ""


def _markdown_blocks(lines: list[str], depth: int = 0) -> str:
    if depth > 64:
        return _paragraph(" ".join(lines), False)
    output: list[str] = []
    index = 0
    while index < len(lines):
        line = lines[index]
        if not line.strip():
            index += 1
            continue
        fence = _FENCE.fullmatch(line)
        if fence:
            marker = fence.group(1)
            index += 1
            code: list[str] = []
            while index < len(lines) and not re.fullmatch(
                re.escape(marker[0]) + r"{" + str(len(marker)) + r",}", lines[index].strip()
            ):
                code.append(lines[index])
                index += 1
            index += 1
            output.append("<pre>" + _escape_text("\n".join(code)) + "</pre>")
            continue
        heading = _HEADING.fullmatch(line)
        if heading:
            level = len(heading.group(1))
            output.append(f"<h{level}>" + _inline_storage(heading.group(2) or "") + f"</h{level}>")
            index += 1
            continue
        if _RULE.fullmatch(line):
            output.append("<hr />")
            index += 1
            continue
        if line.lstrip().startswith(">") and len(line) - len(line.lstrip()) <= 3:
            quoted: list[str] = []
            while index < len(lines) and lines[index].lstrip().startswith(">"):
                quoted.append(re.sub(r"^ {0,3}> ?", "", lines[index]))
                index += 1
            output.append("<blockquote>" + _markdown_blocks(quoted, depth + 1) + "</blockquote>")
            continue
        list_match = _LIST.fullmatch(line)
        if list_match:
            ordered = list_match.group(1)[0].isdigit()
            first_number = int(list_match.group(1)[:-1]) if ordered else 1
            items: list[str] = []
            marker = list_match.group(1)
            while index < len(lines):
                current = _LIST.fullmatch(lines[index])
                if current is None or current.group(1)[0].isdigit() != ordered:
                    break
                if not ordered and current.group(1) != marker:
                    break
                content = current.group(2)
                index += 1
                nested: list[str] = []
                while index < len(lines) and lines[index].startswith("  ") and lines[index].strip():
                    nested.append(lines[index][2:])
                    index += 1
                items.append(
                    _inline_storage(content.strip()) + (_markdown_blocks(nested, depth + 1) if nested else "")
                )
            tag = "ol" if ordered else "ul"
            start = f' start="{first_number}"' if ordered and first_number > 1 else ""
            output.append(f"<{tag}{start}>" + "".join(f"<li>{item}</li>" for item in items) + f"</{tag}>")
            continue
        paragraph: list[str] = []
        while index < len(lines) and lines[index].strip():
            if paragraph and (
                _HEADING.fullmatch(lines[index])
                or _LIST.fullmatch(lines[index])
                or _FENCE.fullmatch(lines[index])
            ):
                break
            paragraph.append(lines[index].strip())
            index += 1
        output.append(_paragraph("\n".join(paragraph), False))
    return "".join(output)


def written_body(body: str, format_name: str | None) -> tuple[str, str]:
    """Validate and convert Markdown or plain text to Confluence storage HTML."""
    try:
        body.encode("utf-8")
    except UnicodeEncodeError as error:
        raise ValueError("body must be valid UTF-8") from error
    if not body.strip():
        raise ValueError("body is required")
    if len(body) > 262_144:
        raise ValueError("body cannot exceed 262144 characters")
    if any((ord(char) < 32 and char not in "\n\r\t") or ord(char) == 127 for char in body):
        raise ValueError("body cannot contain control characters")
    format_name = format_name or "markdown"
    if format_name not in ("markdown", "plainText"):
        raise ValueError("bodyFormat must be markdown or plainText")
    normalized = body.replace("\r\n", "\n").replace("\r", "\n").strip("\n")
    if format_name == "markdown":
        storage = _markdown_blocks(normalized.split("\n"))
    else:
        storage = "".join(_paragraph(part, True) for part in re.split(r"\n[ \t]*\n+", normalized))
    return storage, comparison_text(storage)


@dataclass(slots=True)
class _Node:
    tag: str
    attrs: dict[str, str] = field(default_factory=dict)
    children: list[_Node | str] = field(default_factory=list)


class _TreeParser(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.root = _Node("root")
        self.stack = [self.root]

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        node = _Node(tag.lower(), {key.lower(): value or "" for key, value in attrs})
        self.stack[-1].children.append(node)
        if tag.lower() not in ("br", "hr", "img", "ri:page", "ri:attachment", "ri:url"):
            self.stack.append(node)

    def handle_startendtag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        node = _Node(tag.lower(), {key.lower(): value or "" for key, value in attrs})
        self.stack[-1].children.append(node)

    def handle_endtag(self, tag: str) -> None:
        for index in range(len(self.stack) - 1, 0, -1):
            if self.stack[index].tag == tag.lower():
                self.stack = self.stack[:index]
                break

    def handle_data(self, data: str) -> None:
        self.stack[-1].children.append(data)


def _node_text(node: _Node | str) -> str:
    if isinstance(node, str):
        return node
    return "".join(_node_text(child) for child in node.children)


def _escape_markdown(value: str) -> str:
    return re.sub(r"([\\`*\[\]~])", r"\\\1", value)


def _inline_text(node: _Node | str, markdown: bool) -> str:
    if isinstance(node, str):
        value = re.sub(r"[ \t\r\n\u00a0]+", " ", node)
        return _escape_markdown(value) if markdown else value
    if node.tag == "br":
        return "\n"
    if node.tag in ("ac:image", "ri:attachment", "ac:placeholder", "ac:parameter"):
        return ""
    if node.tag == "ac:link":
        label = next(
            (
                _node_text(child)
                for child in node.children
                if isinstance(child, _Node) and child.tag in ("ac:plain-text-link-body", "ac:link-body")
            ),
            "",
        )
        if not label:
            label = next(
                (
                    child.attrs.get("ri:content-title", "")
                    for child in node.children
                    if isinstance(child, _Node) and child.tag == "ri:page"
                ),
                "",
            )
        return label
    if node.tag == "ac:structured-macro" and node.attrs.get("ac:name") == "status":
        return next(
            (
                _node_text(child)
                for child in node.children
                if isinstance(child, _Node)
                and child.tag == "ac:parameter"
                and child.attrs.get("ac:name") == "title"
            ),
            "",
        )
    text = "".join(_inline_text(child, markdown) for child in node.children)
    if not markdown or not text.strip():
        return text
    if node.tag in ("strong", "b"):
        return "**" + text.strip() + "**"
    if node.tag in ("em", "i"):
        return "*" + text.strip() + "*"
    if node.tag in ("code", "tt"):
        return "`" + text.strip() + "`"
    if node.tag in ("s", "del", "strike") or (
        node.tag == "span" and "line-through" in node.attrs.get("style", "")
    ):
        return "~~" + text.strip() + "~~"
    if node.tag == "a":
        href = _safe_link(node.attrs.get("href", ""))
        return f"[{text.strip()}]({href})" if href else text
    return text


def _render_block(node: _Node, markdown: bool) -> str:
    tag = node.tag
    if tag == "p":
        return "".join(_inline_text(child, markdown) for child in node.children).strip()
    if re.fullmatch(r"h[1-6]", tag):
        text = "".join(_inline_text(child, markdown) for child in node.children).strip()
        return ("#" * int(tag[1]) + " " + text) if markdown else text
    if tag in ("ul", "ol"):
        items = [child for child in node.children if isinstance(child, _Node) and child.tag == "li"]
        start = int(node.attrs.get("start", "1")) if node.attrs.get("start", "1").isdigit() else 1
        return "\n".join(
            (f"{start + index}. " if tag == "ol" else "- ")
            + "".join(_inline_text(part, markdown) for part in item.children).strip()
            for index, item in enumerate(items)
        )
    if tag == "ac:task-list":
        tasks = [child for child in node.children if isinstance(child, _Node) and child.tag == "ac:task"]
        lines = []
        for task in tasks:
            status = next(
                (
                    _node_text(part).strip()
                    for part in task.children
                    if isinstance(part, _Node) and part.tag == "ac:task-status"
                ),
                "",
            )
            body = next(
                (
                    _node_text(part).strip()
                    for part in task.children
                    if isinstance(part, _Node) and part.tag == "ac:task-body"
                ),
                "",
            )
            lines.append(("- [x] " if status == "complete" else "- [ ] ") + body if markdown else "- " + body)
        return "\n".join(lines)
    if tag == "table":
        rows: list[_Node] = []
        for child in node.children:
            if not isinstance(child, _Node):
                continue
            rows.extend(
                [child]
                if child.tag == "tr"
                else [part for part in child.children if isinstance(part, _Node) and part.tag == "tr"]
            )
        rendered: list[str] = []
        for index, row in enumerate(rows):
            cells = [
                child for child in row.children if isinstance(child, _Node) and child.tag in ("th", "td")
            ]
            values = [_node_text(cell).strip().replace("|", "\\|") for cell in cells]
            rendered.append("| " + " | ".join(values) + " |" if markdown else " | ".join(values))
            if markdown and index == 0:
                rendered.append("| " + " | ".join("---" for _ in values) + " |")
        return "\n".join(rendered)
    if tag == "blockquote":
        text = _render_blocks(node.children, markdown)
        return "\n".join(("> " + line if line else ">") for line in text.split("\n")) if markdown else text
    if tag == "pre":
        text = _node_text(node).strip("\n")
        return f"```\n{text}\n```" if markdown else text
    if tag == "hr":
        return "---"
    if tag == "ac:structured-macro":
        name = node.attrs.get("ac:name", "")
        rich = next(
            (
                child
                for child in node.children
                if isinstance(child, _Node) and child.tag == "ac:rich-text-body"
            ),
            None,
        )
        plain = next(
            (
                child
                for child in node.children
                if isinstance(child, _Node) and child.tag == "ac:plain-text-body"
            ),
            None,
        )
        if name in ("code", "noformat") and plain:
            text = _node_text(plain).strip("\n")
            language = next(
                (
                    _node_text(child).strip().lower()
                    for child in node.children
                    if isinstance(child, _Node)
                    and child.tag == "ac:parameter"
                    and child.attrs.get("ac:name") == "language"
                ),
                "",
            )
            return f"```{language}\n{text}\n```" if markdown else text
        if rich:
            text = _render_blocks(rich.children, markdown)
            return (
                "\n".join("> " + line if line else ">" for line in text.split("\n"))
                if name in ("info", "note", "warning", "tip", "panel") and markdown
                else text
            )
        return _node_text(plain).strip() if plain else ""
    return _render_blocks(node.children, markdown)


def _render_blocks(nodes: list[_Node | str], markdown: bool) -> str:
    blocks: list[str] = []
    pending: list[str] = []
    for node in nodes:
        if isinstance(node, _Node) and node.tag in _BLOCK:
            if "".join(pending).strip():
                blocks.append("".join(pending).strip())
            pending = []
            text = _render_block(node, markdown)
            if text.strip():
                blocks.append(text.strip())
        else:
            pending.append(_inline_text(node, markdown))
    if "".join(pending).strip():
        blocks.append("".join(pending).strip())
    return "\n\n".join(blocks)


def storage_text(storage: str, format_name: str = "markdown") -> str:
    """Render bounded Confluence storage as Markdown or plain text."""
    if len(storage.encode("utf-8")) > 16_000_000:
        raise ValueError("body is not readable Confluence storage format")
    parser = _TreeParser()
    parser.feed(storage)
    return _render_blocks(parser.root.children, format_name == "markdown").strip()


def _adf_storage(node: object, depth: int = 0) -> str:
    if depth > 64 or not isinstance(node, dict):
        return ""
    node_type = node.get("type")
    raw_attrs = node.get("attrs")
    attrs = raw_attrs if isinstance(raw_attrs, dict) else {}
    raw_children = node.get("content")
    children = raw_children if isinstance(raw_children, list) else []
    inner = "".join(_adf_storage(child, depth + 1) for child in children)
    if node_type == "text":
        text = node.get("text", "")
        if not isinstance(text, str):
            raise ValueError("invalid ADF text")
        inner = _escape_text(text)
        raw_marks = node.get("marks")
        marks = raw_marks if isinstance(raw_marks, list) else []
        for mark in marks:
            if not isinstance(mark, dict):
                continue
            mark_type = mark.get("type")
            if mark_type in ("strong", "em", "code"):
                inner = f"<{mark_type}>{inner}</{mark_type}>"
            elif mark_type == "strike":
                inner = f"<s>{inner}</s>"
            elif mark_type == "link":
                raw_mark_attrs = mark.get("attrs")
                mark_attrs = raw_mark_attrs if isinstance(raw_mark_attrs, dict) else {}
                href = _safe_link(str(mark_attrs.get("href", "")))
                if href:
                    inner = f'<a href="{_escape_attribute(href)}">{inner}</a>'
        return inner
    if node_type == "hardBreak":
        return "<br />"
    if node_type in ("mention", "status", "emoji"):
        return _escape_text(str(attrs.get("text", attrs.get("shortName", ""))))
    if node_type == "date":
        try:
            date = datetime.fromtimestamp(int(attrs.get("timestamp", "")) / 1000, UTC).date().isoformat()
        except (ValueError, TypeError, OverflowError):
            date = ""
        return _escape_text(date)
    if node_type == "paragraph":
        return f"<p>{inner}</p>"
    if node_type == "heading":
        level = attrs.get("level")
        level = level if type(level) is int and 1 <= level <= 6 else 1
        return f"<h{level}>{inner}</h{level}>"
    if node_type in ("bulletList", "orderedList"):
        tag = "ol" if node_type == "orderedList" else "ul"
        start = attrs.get("order", 1)
        start_attribute = f' start="{start}"' if tag == "ol" and type(start) is int and start > 1 else ""
        return f"<{tag}{start_attribute}>{inner}</{tag}>"
    if node_type == "listItem":
        return f"<li>{inner}</li>"
    if node_type == "taskList":
        return f"<ac:task-list>{inner}</ac:task-list>"
    if node_type == "taskItem":
        state = "complete" if attrs.get("state") == "DONE" else "incomplete"
        return (
            f"<ac:task><ac:task-status>{state}</ac:task-status><ac:task-body>{inner}</ac:task-body></ac:task>"
        )
    if node_type in ("blockquote", "panel"):
        return f"<blockquote>{inner}</blockquote>"
    if node_type == "codeBlock":
        language = str(attrs.get("language", "")).strip().lower()
        language = language if re.fullmatch(r"[a-z0-9+#._-]{0,32}", language) else ""
        return (
            f'<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">'
            f"{_escape_text(language)}</ac:parameter><ac:plain-text-body>"
            f"{inner}</ac:plain-text-body></ac:structured-macro>"
        )
    if node_type == "rule":
        return "<hr />"
    if node_type == "table":
        return f"<table><tbody>{inner}</tbody></table>"
    if node_type == "tableRow":
        return f"<tr>{inner}</tr>"
    if node_type in ("tableHeader", "tableCell"):
        tag = "th" if node_type == "tableHeader" else "td"
        return f"<{tag}>{inner}</{tag}>"
    if node_type in ("mediaSingle", "mediaGroup", "media", "extension"):
        return ""
    return inner


def adf_text(value: str, format_name: str = "plainText") -> str:
    """Read supported Atlassian Document Format blocks as Markdown or plain text."""
    document = json.loads(value)
    if not isinstance(document, dict) or document.get("type") != "doc":
        raise ValueError("invalid ADF document")
    return storage_text(_adf_storage(document), format_name)


def comparison_text(storage: str) -> str:
    """Compare provider-normalized body text without formatting whitespace."""
    return "".join(storage_text(storage, "plainText").split())
