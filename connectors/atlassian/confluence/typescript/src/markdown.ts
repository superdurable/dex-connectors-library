// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { appendInline, appendParagraphBlock, documentComparisonText, markdownFenceLanguage, renderStorageDocument, safeLinkHref, trimInlineEdges, type DocumentBlock, type DocumentInline, type DocumentListItem } from './document.js';

const headingPattern = /^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$/;
const thematicBreakPattern = /^ {0,3}(?:(?:\*[ \t]*){3,}|(?:-[ \t]*){3,}|(?:_[ \t]*){3,})$/;
const fenceOpeningPattern = /^( {0,3})(`{3,}|~{3,})[ \t]*([^`]*?)[ \t]*$/;
const listMarkerPattern = /^( {0,3})([-*+]|[0-9]{1,9}[.)])([ \t]+|$)/;
const tableSeparatorPattern = /^ {0,3}\|?[ \t]*:?-+:?[ \t]*(\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$/;
const taskMarkerPattern = /^\[([ xX])\][ \t]+/;

/** Validate and convert Markdown or plain text to Confluence storage XHTML and comparison text. */
export function convertWrittenBody(body: string, format: string | undefined, fieldName: string): { storage: string; comparisonText: string } {
  if (!body.trim()) throw new TypeError(`${fieldName} is required`);
  if ([...body].length > 262144) throw new TypeError(`${fieldName} cannot exceed 262144 characters`);
  if ([...body].some(character => { const code = character.codePointAt(0)!; return code === 127 || code < 32 && code !== 9 && code !== 10 && code !== 13; })) throw new TypeError(`${fieldName} cannot contain control characters`);
  const normalized = body.replace(/\r\n?/g, '\n');
  const blocks = !format || format === 'markdown' ? readMarkdownBlocks(normalized.replace(/^\n+|\n+$/g, '').split('\n'), 0)
    : format === 'plainText' ? readPlainText(normalized) : undefined;
  if (!blocks) throw new TypeError(`${fieldName}Format must be markdown or plainText`);
  return { storage: renderStorageDocument(blocks), comparisonText: documentComparisonText(blocks) };
}

function readPlainText(text: string): DocumentBlock[] {
  return text.replace(/^\n+|\n+$/g, '').split(/\n[ \t]*\n+/).reduce((blocks, paragraph) => {
    let inlines: DocumentInline[] = [];
    for (const [index, line] of paragraph.split('\n').entries()) {
      if (index) inlines = appendInline(inlines, { isLineBreak: true });
      inlines = appendInline(inlines, { text: line });
    }
    return appendParagraphBlock(blocks, inlines);
  }, [] as DocumentBlock[]);
}
function readMarkdownBlocks(lines: string[], depth: number): DocumentBlock[] {
  const blocks: DocumentBlock[] = [];
  for (let index = 0; index < lines.length;) {
    const line = lines[index]!;
    if (depth > 64) { blocks.push({ kind: 'paragraph', inlines: [{ text: lines.slice(index).join(' ') }] }); break; }
    if (!line.trim()) { index++; continue; }
    const fence = line.match(fenceOpeningPattern);
    if (fence) {
      const marker = fence[2]!, indentation = fence[1]!.length;
      const language = markdownFenceLanguage((fence[3] || '').trim().split(' ')[0] || '');
      const code: string[] = []; index++;
      while (index < lines.length) {
        const candidate = lines[index]!.trim();
        if (candidate.startsWith(marker[0]!) && [...candidate].every(character => character === marker[0]) && candidate.length >= marker.length) { index++; break; }
        code.push(removeIndentation(lines[index]!, indentation)); index++;
      }
      blocks.push({ kind: 'code', codeText: code.join('\n'), codeLanguage: language }); continue;
    }
    const heading = line.match(headingPattern);
    if (heading) { blocks.push({ kind: 'heading', headingLevel: heading[1]!.length, inlines: readMarkdownInlines(heading[2] || '', {}, 0) }); index++; continue; }
    if (thematicBreakPattern.test(line)) { blocks.push({ kind: 'rule' }); index++; continue; }
    if (isBlockQuoteLine(line)) {
      const quoted: string[] = [];
      while (index < lines.length && isBlockQuoteLine(lines[index]!)) quoted.push(lines[index++]!.trimStart().replace(/^> ?/, ''));
      blocks.push({ kind: 'quote', children: readMarkdownBlocks(quoted, depth + 1) }); continue;
    }
    if (listMarkerPattern.test(line)) {
      const [block, next] = readList(lines, index, depth); blocks.push(block); index = next; continue;
    }
    if (index + 1 < lines.length && line.includes('|') && lines[index + 1]!.includes('|') && tableSeparatorPattern.test(lines[index + 1]!)) {
      const rows = [{ isHeader: true, cells: readTableCells(line) }]; index += 2;
      while (index < lines.length && lines[index]!.includes('|') && lines[index]!.trim()) rows.push({ isHeader: false, cells: readTableCells(lines[index++]!) });
      blocks.push({ kind: 'table', tableRows: rows }); continue;
    }
    const paragraphLines: string[] = [];
    while (index < lines.length && lines[index]!.trim() && (!paragraphLines.length || !startsMarkdownBlock(lines[index]!))) paragraphLines.push(lines[index++]!.trim());
    const paragraph = appendParagraphBlock([], readMarkdownInlines(paragraphLines.join('\n'), {}, 0)); blocks.push(...paragraph);
  }
  return blocks;
}
function readList(lines: string[], start: number, depth: number): [DocumentBlock, number] {
  const first = lines[start]!.match(listMarkerPattern)!;
  const isOrdered = /^[0-9]/.test(first[2]!);
  const block: DocumentBlock = { kind: isOrdered ? 'orderedList' : 'bulletList', orderedStart: isOrdered ? Number.parseInt(first[2]!, 10) : 0, listItems: [] };
  let index = start; const groups: string[][] = [];
  while (index < lines.length) {
    const marker = lines[index]!.match(listMarkerPattern);
    if (!marker || /^[0-9]/.test(marker[2]!) !== isOrdered || (!isOrdered && marker[2] !== first[2])) break;
    let indentation = marker[0].length;
    if (!marker[3]!.trim() && marker[3]!.length > 4) indentation = marker[1]!.length + marker[2]!.length + 1;
    const itemLines = [lines[index]!.slice(marker[0].length)]; index++;
    while (index < lines.length) {
      const next = lines[index]!;
      if (!next.trim()) { if (index + 1 < lines.length && leadingSpaces(lines[index + 1]!) >= indentation) { itemLines.push(''); index++; continue; } break; }
      if (leadingSpaces(next) < indentation) break;
      itemLines.push(removeIndentation(next, indentation)); index++;
    }
    groups.push(itemLines);
    while (index < lines.length && !lines[index]!.trim() && index + 1 < lines.length && listMarkerPattern.test(lines[index + 1]!)) index++;
  }
  const isTask = !isOrdered && groups.every(group => taskMarkerPattern.test(group[0]!));
  block.listItems = groups.map(group => {
    let isTaskDone = false;
    if (isTask) { const match = group[0]!.match(taskMarkerPattern)!; isTaskDone = match[1] !== ' '; group[0] = group[0]!.slice(match[0].length); }
    return { blocks: readMarkdownBlocks(group, depth + 1), isTaskDone } as DocumentListItem;
  });
  if (isTask) block.kind = 'taskList';
  return [block, index];
}
function readTableCells(line: string): DocumentInline[][] {
  let value = line.trim().replace(/^\|/, '');
  if (value.endsWith('|') && !value.endsWith('\\|')) value = value.slice(0, -1);
  const cells: DocumentInline[][] = []; let cell = '', escaped = false;
  for (const character of value) {
    if (escaped) { cell += (character === '|' ? '' : '\\') + character; escaped = false; }
    else if (character === '\\') escaped = true;
    else if (character === '|') { cells.push(readMarkdownInlines(cell.trim(), {}, 0)); cell = ''; }
    else cell += character;
  }
  cells.push(readMarkdownInlines(cell.trim(), {}, 0)); return cells;
}
function readMarkdownInlines(value: string, style: DocumentInline, depth: number): DocumentInline[] {
  const characters = [...value]; let inlines: DocumentInline[] = [], pending = '';
  const flush = () => { inlines = appendInline(inlines, { ...style, text: pending }); pending = ''; };
  for (let index = 0; index < characters.length;) {
    const character = characters[index]!;
    if (depth >= 32) { pending += character; index++; continue; }
    if (character === '\\' && index + 1 < characters.length && isMarkdownPunctuation(characters[index + 1]!)) { pending += characters[index + 1]!; index += 2; continue; }
    if (character === '\n') { flush(); inlines = appendInline(inlines, { isLineBreak: true }); index++; continue; }
    if (character === '`') {
      const code = readCodeSpan(characters, index);
      if (code) { flush(); inlines = appendInline(inlines, { ...style, text: code.text, isCode: true }); index = code.end; continue; }
      const length = delimiterRunLength(characters, index, character); pending += character.repeat(length); index += length; continue;
    }
    if (character === '[') {
      const link = readMarkdownLink(characters, index);
      if (link) { flush(); for (const span of readMarkdownInlines(link.label, { ...style, linkHref: safeLinkHref(link.href) }, depth + 1)) inlines = appendInline(inlines, span); index = link.end; continue; }
    }
    if (character === '<') {
      const end = characters.indexOf('>', index + 1);
      if (end >= 0) { const href = characters.slice(index + 1, end).join(''); if (href && safeLinkHref(href) === href) { flush(); inlines = appendInline(inlines, { ...style, text: href, linkHref: href }); index = end + 1; continue; } }
    }
    if ('*_~'.includes(character)) {
      const emphasis = readEmphasis(characters, index, style);
      if (emphasis) { flush(); for (const span of readMarkdownInlines(emphasis.inner, emphasis.style, depth + 1)) inlines = appendInline(inlines, span); index = emphasis.end; continue; }
      const length = delimiterRunLength(characters, index, character); pending += character.repeat(length); index += length; continue;
    }
    pending += character; index++;
  }
  flush(); return trimInlineEdges(inlines);
}
function readCodeSpan(characters: string[], start: number): { text: string; end: number } | undefined {
  const length = delimiterRunLength(characters, start, '`');
  for (let index = start + length; index < characters.length;) {
    if (characters[index] !== '`') { index++; continue; }
    const closing = delimiterRunLength(characters, index, '`');
    if (closing === length) {
      let text = characters.slice(start + length, index).join('');
      if (text.length >= 2 && text.startsWith(' ') && text.endsWith(' ') && text.trim()) text = text.slice(1, -1);
      return { text: text.replace(/\n/g, ' '), end: index + closing };
    }
    index += closing;
  }
  return undefined;
}
function readMarkdownLink(characters: string[], start: number): { label: string; href: string; end: number } | undefined {
  let depth = 0;
  for (let index = start; index < characters.length; index++) {
    if (characters[index] === '\\') { index++; continue; }
    if (characters[index] === '[') depth++;
    if (characters[index] !== ']') continue;
    depth--; if (depth > 0) continue;
    if (characters[index + 1] !== '(') return undefined;
    const closing = balancedClosing(characters, index + 2); if (closing < 0) return undefined;
    const href = characters.slice(index + 2, closing).join('').trim().replace(/^</, '').replace(/>$/, '');
    if (!href || /[ \t\n]/.test(href)) return undefined;
    return { label: characters.slice(start + 1, index).join(''), href, end: closing + 1 };
  }
  return undefined;
}
function balancedClosing(characters: string[], start: number): number {
  let depth = 0;
  for (let index = start; index < characters.length; index++) {
    const character = characters[index];
    if (character === '\\') { index++; continue; }
    if (character === '(') depth++;
    if (character === ')') { if (!depth) return index; depth--; }
    if (character === '\n') return -1;
  }
  return -1;
}
function readEmphasis(characters: string[], start: number, style: DocumentInline): { inner: string; style: DocumentInline; end: number } | undefined {
  const character = characters[start]!, runLength = delimiterRunLength(characters, start, character);
  if (character === '~' && runLength < 2) return undefined;
  const length = Math.min(runLength, 2), contentStart = start + length;
  if (contentStart >= characters.length || /\s/u.test(characters[contentStart]!)) return undefined;
  if (character === '_' && start > 0 && isWord(characters[start - 1]!)) return undefined;
  for (let index = contentStart + 1; index < characters.length; index++) {
    if (characters[index] === '\\') { index++; continue; }
    if (characters[index] !== character) continue;
    const closingRun = delimiterRunLength(characters, index, character);
    if (closingRun < length || /\s/u.test(characters[index - 1]!)) { index += closingRun - 1; continue; }
    const closing = index + closingRun - length, end = closing + length;
    if (character === '_' && end < characters.length && isWord(characters[end]!)) { index += closingRun - 1; continue; }
    const nextStyle = { ...style, ...(character === '~' ? { isStrikethrough: true } : length === 2 ? { isStrong: true } : { isEmphasis: true }) };
    return { inner: characters.slice(contentStart, closing).join(''), style: nextStyle, end };
  }
  return undefined;
}
function isWord(character: string): boolean { return /[\p{L}\p{N}]/u.test(character); }
function isMarkdownPunctuation(character: string): boolean { return /^[!-/:-@[-`{-~]$/.test(character) || '`$^+<=>|~'.includes(character); }
function delimiterRunLength(characters: string[], start: number, character: string): number { let length = 0; while (characters[start + length] === character) length++; return length; }
function isBlockQuoteLine(line: string): boolean { return line.trimStart().startsWith('>') && leadingSpaces(line) <= 3; }
function startsMarkdownBlock(line: string): boolean { return fenceOpeningPattern.test(line) || headingPattern.test(line) || thematicBreakPattern.test(line) || isBlockQuoteLine(line) || listMarkerPattern.test(line); }
function leadingSpaces(line: string): number { return line.match(/^ */)?.[0].length || 0; }
function removeIndentation(line: string, amount: number): string { return line.slice(Math.min(leadingSpaces(line), amount)); }
