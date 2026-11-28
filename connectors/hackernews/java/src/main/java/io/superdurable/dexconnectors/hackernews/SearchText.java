// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import nu.validator.htmlparser.common.TokenHandler;
import nu.validator.htmlparser.impl.ElementName;
import nu.validator.htmlparser.impl.HtmlAttributes;
import nu.validator.htmlparser.impl.Tokenizer;
import nu.validator.htmlparser.io.Driver;

import org.xml.sax.InputSource;
import org.xml.sax.SAXException;

import java.io.IOException;
import java.io.StringReader;
import java.util.*;
import java.util.regex.Pattern;

final class SearchText {
    final String value;
    final boolean truncated;

    private SearchText(String plain) {
        int length = plain.codePointCount(0, plain.length());
        truncated = length > SearchQueries.MAX_STORY_TEXT_CHARACTERS;
        value =
                truncated
                        ? plain.substring(
                                0,
                                plain.offsetByCodePoints(
                                        0, SearchQueries.MAX_STORY_TEXT_CHARACTERS))
                        : plain;
    }

    static SearchText fromHtml(String html) throws IOException {
        if (html == null || html.isEmpty()) return new SearchText("");
        Paragraphs paragraphs = new Paragraphs();
        try {
            new Driver(new Tokenizer(paragraphs)).tokenize(new InputSource(new StringReader(html)));
        } catch (SAXException error) {
            throw new IOException("invalid indexed text", error);
        }
        return new SearchText(String.join("\n\n", paragraphs.output));
    }

    private static final class Paragraphs implements TokenHandler {
        private static final List<String> BLOCKS =
                Arrays.asList("p", "pre", "br", "div", "li", "blockquote");
        private static final Pattern WHITESPACE = Pattern.compile("(?U)\\s+");
        final List<String> output = new ArrayList<>();
        final StringBuilder current = new StringBuilder();
        private Tokenizer tokenizer;

        public void startTokenization(Tokenizer self) {
            tokenizer = self;
        }

        public void characters(char[] text, int start, int length) {
            current.append(text, start, length);
        }

        public void startTag(ElementName name, HtmlAttributes attributes, boolean selfClosing) {
            String tag = name.getName();
            if (BLOCKS.contains(tag)) finishParagraph();
            if (selfClosing) return;
            switch (tag) {
                case "script":
                    tokenizer.setStateAndEndTagExpectation(Tokenizer.SCRIPT_DATA, name);
                    break;
                case "style":
                case "xmp":
                case "iframe":
                case "noembed":
                case "noframes":
                case "noscript":
                    tokenizer.setStateAndEndTagExpectation(Tokenizer.RAWTEXT, name);
                    break;
                case "textarea":
                case "title":
                    tokenizer.setStateAndEndTagExpectation(Tokenizer.RCDATA, name);
                    break;
                case "plaintext":
                    tokenizer.setStateAndEndTagExpectation(Tokenizer.PLAINTEXT, name);
                    break;
                default:
                    break;
            }
        }

        public void endTag(ElementName name) {
            if (BLOCKS.contains(name.getName())) finishParagraph();
        }

        public void eof() {
            finishParagraph();
        }

        private void finishParagraph() {
            String text = WHITESPACE.matcher(current).replaceAll(" ");
            int start = text.startsWith(" ") ? 1 : 0;
            int end = text.endsWith(" ") ? text.length() - 1 : text.length();
            if (end > start) output.add(text.substring(start, end));
            current.setLength(0);
        }

        public void zeroOriginatingReplacementCharacter() {
            current.append('\uFFFD');
        }

        public boolean wantsComments() {
            return false;
        }

        public boolean cdataSectionAllowed() {
            return false;
        }

        public void doctype(
                String name, String publicIdentifier, String systemIdentifier, boolean quirks) {}

        public void comment(char[] text, int start, int length) {}

        public void endTokenization() {}

        public void ensureBufferSpace(int length) {}
    }
}
