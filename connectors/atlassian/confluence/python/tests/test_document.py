# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Match Confluence's Go document conversion on representative rich content."""

import pytest

from dex_connectors.atlassian_confluence.document import adf_text, storage_text, written_body


def test_markdown_writes_inline_styles_links_lists_and_code() -> None:
    """Written Markdown becomes escaped Confluence storage XHTML."""
    storage, _ = written_body(
        "# Remote work\n\nStaff work **remotely** two *days*. See [the handbook](https://example.com/a?b=1&c=2).\n\n"
        "- Ask your manager\n- Log the days\n\n```go\nx := 1\n```",
        "markdown",
    )
    assert storage == (
        "<h1>Remote work</h1><p>Staff work <strong>remotely</strong> two <em>days</em>. "
        'See <a href="https://example.com/a?b=1&amp;c=2">the handbook</a>.</p>'
        "<ul><li>Ask your manager</li><li>Log the days</li></ul><pre>x := 1</pre>"
    )


def test_plain_text_keeps_line_breaks_and_escapes_markup() -> None:
    """Plain text cannot introduce a heading or HTML markup."""
    storage, _ = written_body("\r\n# Not a heading\r\n**not bold** <b>\n\nNew paragraph\n", "plainText")
    assert storage == "<p># Not a heading<br />**not bold** &lt;b&gt;</p><p>New paragraph</p>"


def test_storage_reads_markdown_and_plain_text() -> None:
    """Read common Confluence block and inline formatting."""
    storage = "<h1>Title</h1><p>One <em>two</em><br />three</p><ul><li>item</li></ul><pre>code  kept</pre>"
    assert storage_text(storage, "markdown") == (
        "# Title\n\nOne *two*\nthree\n\n- item\n\n```\ncode  kept\n```"
    )
    assert storage_text(storage, "plainText") == "Title\n\nOne two\nthree\n\n- item\n\ncode  kept"


def test_written_text_rejects_control_characters() -> None:
    """Invalid caller text is a validation branch before provider dispatch."""
    with pytest.raises(ValueError, match="control"):
        written_body("bell\a", "markdown")


def test_adf_rejects_non_document() -> None:
    """A malformed ADF root is an invalid provider response."""
    with pytest.raises(ValueError):
        adf_text('{"type":"paragraph"}')


def test_adf_reads_rich_blocks_as_markdown() -> None:
    """ADF headings, marks, lists, panels, code, and rules keep their meaning."""
    document = {
        "type": "doc",
        "content": [
            {"type": "heading", "attrs": {"level": 3}, "content": [{"type": "text", "text": "Impact"}]},
            {
                "type": "paragraph",
                "content": [
                    {"type": "text", "text": "Bold", "marks": [{"type": "strong"}]},
                    {"type": "text", "text": " and "},
                    {
                        "type": "text",
                        "text": "site",
                        "marks": [{"type": "link", "attrs": {"href": "https://example.com"}}],
                    },
                ],
            },
            {
                "type": "orderedList",
                "attrs": {"order": 2},
                "content": [
                    {
                        "type": "listItem",
                        "content": [{"type": "paragraph", "content": [{"type": "text", "text": "two"}]}],
                    }
                ],
            },
            {
                "type": "taskList",
                "content": [
                    {
                        "type": "taskItem",
                        "attrs": {"state": "DONE"},
                        "content": [{"type": "text", "text": "done"}],
                    }
                ],
            },
            {
                "type": "panel",
                "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Note"}]}],
            },
            {
                "type": "codeBlock",
                "attrs": {"language": "sql"},
                "content": [{"type": "text", "text": "select 1"}],
            },
            {"type": "rule"},
        ],
    }
    import json

    assert adf_text(json.dumps(document), "markdown") == (
        "### Impact\n\n**Bold** and [site](https://example.com)\n\n2. two\n\n"
        "- [x] done\n\n> Note\n\n```sql\nselect 1\n```\n\n---"
    )
