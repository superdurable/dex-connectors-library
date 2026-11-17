// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Parser } from 'htmlparser2';
import { decodeHTMLBytes } from './html_head.js';
import { collapseWhitespace, goTrimRight } from './text.js';

const boilerplateElements = new Set([
  'script', 'style', 'noscript', 'template', 'svg', 'math', 'iframe', 'title', 'nav', 'header', 'footer', 'aside', 'button', 'select', 'textarea',
]);
// Go's x/net/html atom table: the block elements that end a paragraph.
const blockElements = new Set([
  'address', 'article', 'aside', 'blockquote', 'br', 'dd', 'details', 'div', 'dl', 'dt', 'figcaption', 'figure', 'footer',
  'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'header', 'hr', 'img', 'li', 'main', 'nav', 'ol', 'p', 'pre', 'section', 'summary',
  'table', 'td', 'th', 'tr', 'ul',
]);

/** One block of page text and the outermost article and main elements around it; zero means outside. */
interface ReadableParagraph { text: string; articleOrdinal: number; mainOrdinal: number }

/**
 * Return the paragraphs of the article with the most text, else of the main element with the most text, else
 * of the whole page, separated by one blank line. htmlparser2 delivers balanced element events with the same
 * implied-close rules the Go connector applies, so both packages extract equal text.
 */
export function extractReadableText(body: Uint8Array, contentType: string): string {
  const collector = new ReadableTextCollector();
  const parser = new Parser({
    onopentag(name) { collector.openElement(name); },
    onclosetag(name) { collector.closeElement(name); },
    ontext(text) { collector.addText(text); },
  }, { decodeEntities: true });
  parser.end(decodePageText(body, contentType));
  collector.finishParagraph();
  return selectReadableParagraphs(collector.paragraphs).map(paragraph => paragraph.text).join('\n\n');
}

/** Decode body as golang.org/x/net/html/charset does; TextDecoder drops a leading byte order mark. */
export function decodePageText(body: Uint8Array, contentType: string): string {
  return decodeHTMLBytes(body, contentType);
}

/** Cut text to at most maxCharacters code points at a late space or line break, ending a cut text with an ellipsis. */
export function truncateText(text: string, maxCharacters: number): [string, boolean] {
  const codePoints = Array.from(text);
  if (codePoints.length <= maxCharacters) return [text, false];
  let cut = Buffer.from(codePoints.slice(0, maxCharacters - 1).join(''), 'utf8');
  const boundary = Math.max(cut.lastIndexOf(0x20), cut.lastIndexOf(0x0a));
  if (boundary > Math.floor(cut.length / 2)) cut = cut.subarray(0, boundary);
  return [goTrimRight(cut.toString('utf8')) + '…', true];
}

class ReadableTextCollector {
  readonly paragraphs: ReadableParagraph[] = [];
  private lines: string[] = [];
  private line = '';
  private skippedDepth = 0;
  private articleDepth = 0;
  private articleOrdinal = 0;
  private mainDepth = 0;
  private mainOrdinal = 0;

  addText(text: string): void { if (this.skippedDepth === 0) this.line += text; }

  openElement(name: string): void {
    if (boilerplateElements.has(name)) { this.finishParagraph(); this.skippedDepth++; return; }
    if (this.skippedDepth > 0) return;
    if (name === 'br') { this.finishLine(); return; }
    if (name === 'article') {
      this.finishParagraph();
      if (this.articleDepth === 0) this.articleOrdinal++;
      this.articleDepth++;
      return;
    }
    if (name === 'main') {
      this.finishParagraph();
      if (this.mainDepth === 0) this.mainOrdinal++;
      this.mainDepth++;
      return;
    }
    if (blockElements.has(name)) this.finishParagraph();
  }

  closeElement(name: string): void {
    if (boilerplateElements.has(name)) { this.skippedDepth = Math.max(this.skippedDepth - 1, 0); return; }
    if (this.skippedDepth > 0) return;
    if (name === 'br') { this.finishLine(); return; }
    if (name === 'article') { this.finishParagraph(); this.articleDepth = Math.max(this.articleDepth - 1, 0); return; }
    if (name === 'main') { this.finishParagraph(); this.mainDepth = Math.max(this.mainDepth - 1, 0); return; }
    if (blockElements.has(name)) this.finishParagraph();
  }

  finishParagraph(): void {
    this.finishLine();
    if (this.lines.length === 0) return;
    this.paragraphs.push({
      text: this.lines.join('\n'),
      articleOrdinal: this.articleDepth > 0 ? this.articleOrdinal : 0,
      mainOrdinal: this.mainDepth > 0 ? this.mainOrdinal : 0,
    });
    this.lines = [];
  }

  private finishLine(): void {
    const line = collapseWhitespace(this.line);
    if (line !== '') this.lines.push(line);
    this.line = '';
  }
}

function selectReadableParagraphs(paragraphs: ReadableParagraph[]): ReadableParagraph[] {
  return selectLargestRegion(paragraphs, paragraph => paragraph.articleOrdinal)
    ?? selectLargestRegion(paragraphs, paragraph => paragraph.mainOrdinal)
    ?? paragraphs;
}

// The region with the most code points wins; the earliest wins a tie.
function selectLargestRegion(paragraphs: ReadableParagraph[], regionOrdinal: (paragraph: ReadableParagraph) => number): ReadableParagraph[] | undefined {
  const sizes = new Map<number, number>();
  let largestOrdinal = 0;
  for (const paragraph of paragraphs) {
    const ordinal = regionOrdinal(paragraph);
    if (ordinal === 0) continue;
    const size = (sizes.get(ordinal) ?? 0) + Array.from(paragraph.text).length;
    sizes.set(ordinal, size);
    const largest = sizes.get(largestOrdinal) ?? 0;
    if (largestOrdinal === 0 || size > largest || (size === largest && ordinal < largestOrdinal)) largestOrdinal = ordinal;
  }
  if (largestOrdinal === 0) return undefined;
  return paragraphs.filter(paragraph => regionOrdinal(paragraph) === largestOrdinal);
}
