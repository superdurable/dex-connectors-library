# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

# ReadWebPageInput identifies one public HTTPS page.
@dataclass(frozen=True, slots=True)
class ReadWebPageInput:
    # URL is a public HTTPS page with a domain name and no query, fragment, or user info.
    url: str = ''


setattr(ReadWebPageInput, '__module__', 'dex_connectors.dataeyes.models')


# ReadWebPageOutput contains the page title and bounded Markdown text.
@dataclass(frozen=True, slots=True)
class ReadWebPageOutput:
    # Title is the title reported by DataEyes, if available.
    title: str = ''
    # Markdown is the extracted page content, limited to 100000 bytes.
    markdown: str = ''


setattr(ReadWebPageOutput, '__module__', 'dex_connectors.dataeyes.models')
