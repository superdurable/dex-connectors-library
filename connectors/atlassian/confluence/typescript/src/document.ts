// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/** Format-neutral Confluence document node, shared by Markdown, storage, and ADF converters. */
export interface DocumentInline {
  text?: string;
  isLineBreak?: boolean;
  isStrong?: boolean;
  isEmphasis?: boolean;
  isCode?: boolean;
  isStrikethrough?: boolean;
  linkHref?: string;
}
/** One list item in the format-neutral document. */
export interface DocumentListItem { blocks: DocumentBlock[]; isTaskDone?: boolean; }
/** One table row in the format-neutral document. */
export interface DocumentTableRow { cells: DocumentInline[][]; isHeader?: boolean; }
/** One block in the format-neutral document. */
export interface DocumentBlock {
  kind: 'paragraph' | 'heading' | 'bulletList' | 'orderedList' | 'taskList' | 'quote' | 'code' | 'rule' | 'table';
  headingLevel?: number;
  inlines?: DocumentInline[];
  listItems?: DocumentListItem[];
  orderedStart?: number;
  children?: DocumentBlock[];
  codeLanguage?: string;
  codeText?: string;
  tableRows?: DocumentTableRow[];
}

/** Render a page as Markdown or plain text and apply the Go character bound. */
export function renderDocumentText(blocks: DocumentBlock[], format: string, maxCharacters: number): { text: string; isTruncated: boolean } {
  const text = renderBlocks(blocks, format === 'markdown').trim();
  const characters = [...text];
  return maxCharacters > 0 && characters.length > maxCharacters
    ? { text: characters.slice(0, maxCharacters).join(''), isTruncated: true }
    : { text, isTruncated: false };
}

/** Return whitespace-free plain text for read-back attribution. */
export function documentComparisonText(blocks: DocumentBlock[]): string { return renderBlocks(blocks, false).split(/\s+/u).join(''); }

function renderBlocks(blocks: DocumentBlock[], isMarkdown: boolean): string {
  return blocks.map(block => renderBlock(block, isMarkdown)).filter(value => value.trim()).join('\n\n');
}
function renderBlock(block: DocumentBlock, isMarkdown: boolean): string {
  switch (block.kind) {
    case 'paragraph': {
      const text = renderInlines(block.inlines || [], isMarkdown);
      return isMarkdown ? text.split('\n').map(escapeMarkdownLineStart).join('\n') : text;
    }
    case 'heading': {
      const text = renderInlines(block.inlines || [], isMarkdown).replace(/\n/g, ' ');
      return isMarkdown ? '#'.repeat(Math.min(6, Math.max(1, block.headingLevel || 1))) + ' ' + text : text;
    }
    case 'bulletList': case 'orderedList': case 'taskList':
      return (block.listItems || []).map((item, index) => {
        const marker = block.kind === 'orderedList' ? `${Math.max(1, block.orderedStart || 1) + index}. ` : block.kind === 'taskList' ? item.isTaskDone ? '- [x] ' : '- [ ] ' : '- ';
        return marker + renderBlocks(item.blocks, isMarkdown).split('\n').map((line, lineIndex) => lineIndex && line ? ' '.repeat(marker.length) + line : line).join('\n');
      }).join('\n');
    case 'quote': {
      const text = renderBlocks(block.children || [], isMarkdown);
      return isMarkdown ? text.split('\n').map(line => line ? '> ' + line : '>').join('\n') : text;
    }
    case 'code': {
      if (!isMarkdown) return block.codeText || '';
      let fence = '```'; while ((block.codeText || '').includes(fence)) fence += '`';
      return `${fence}${block.codeLanguage || ''}\n${block.codeText || ''}\n${fence}`;
    }
    case 'rule': return '---';
    case 'table': return (block.tableRows || []).flatMap((row, index) => {
      const cells = row.cells.map(cell => renderInlines(cell, isMarkdown).replace(/\n/g, ' ').replace(isMarkdown ? /\|/g : /$^/, '\\|'));
      if (!isMarkdown) return [cells.join(' | ')];
      const line = `| ${cells.join(' | ')} |`;
      return index === 0 ? [line, `| ${cells.map(() => '---').join(' | ')} |`] : [line];
    }).join('\n');
  }
}
function renderInlines(inlines: DocumentInline[], isMarkdown: boolean): string {
  return inlines.map(inline => {
    if (inline.isLineBreak) return '\n';
    const original = inline.text || '';
    if (!isMarkdown || !original.trim()) return original;
    const leading = original.match(/^\s*/u)?.[0] || '';
    const trailing = original.match(/\s*$/u)?.[0] || '';
    const core = original.trim();
    let text = escapeMarkdownText(core);
    if (inline.isCode) { let fence = '`'; while (core.includes(fence)) fence += '`'; text = fence + core + fence; }
    if (inline.isStrikethrough) text = `~~${text}~~`;
    if (inline.isEmphasis) text = `*${text}*`;
    if (inline.isStrong) text = `**${text}**`;
    if (inline.linkHref) text = `[${text}](${inline.linkHref.replace(/\(/g, '%28').replace(/\)/g, '%29').replace(/ /g, '%20')})`;
    return leading + text + trailing;
  }).join('');
}
function escapeMarkdownText(value: string): string {
  const characters = [...value];
  return characters.map((character, index) => {
    if ('\\`*[]~'.includes(character)) return '\\' + character;
    if (character === '_' && !(isWord(characters[index - 1]) && isWord(characters[index + 1]))) return '\\_';
    return character;
  }).join('');
}
function isWord(value?: string): boolean { return !!value && /[\p{L}\p{N}]/u.test(value); }
function escapeMarkdownLineStart(line: string): string {
  const match = line.match(/^( *)(.*)$/);
  if (!match) return line;
  const indentation = match[1] || '', trimmed = match[2] || '';
  if (/^[#>\-+=|]/.test(trimmed)) return indentation + '\\' + trimmed;
  return indentation + trimmed.replace(/^([0-9]+)([.)])/, '$1\\$2');
}

/** Merge adjacent spans with identical style. */
export function appendInline(inlines: DocumentInline[], span: DocumentInline): DocumentInline[] {
  if (span.isLineBreak) return [...inlines, span];
  if (!span.text) return inlines;
  const previous = inlines.at(-1);
  if (previous && !previous.isLineBreak && !!previous.isStrong === !!span.isStrong && !!previous.isEmphasis === !!span.isEmphasis && !!previous.isCode === !!span.isCode && !!previous.isStrikethrough === !!span.isStrikethrough && (previous.linkHref || '') === (span.linkHref || '')) {
    previous.text = (previous.text || '') + span.text;
    return inlines;
  }
  return [...inlines, span];
}
/** Trim empty edges while preserving internal formatting. */
export function trimInlineEdges(inlines: DocumentInline[]): DocumentInline[] {
  const trimmed = [...inlines];
  while (trimmed.length && (trimmed[0]!.isLineBreak || !(trimmed[0]!.text || '').trim())) trimmed.shift();
  while (trimmed.length && (trimmed.at(-1)!.isLineBreak || !(trimmed.at(-1)!.text || '').trim())) trimmed.pop();
  if (!trimmed.length) return [];
  trimmed[0] = { ...trimmed[0]!, text: (trimmed[0]!.text || '').trimStart() };
  trimmed[trimmed.length - 1] = { ...trimmed.at(-1)!, text: (trimmed.at(-1)!.text || '').trimEnd() };
  return trimmed;
}
/** Add a paragraph only when spans hold visible text. */
export function appendParagraphBlock(blocks: DocumentBlock[], inlines: DocumentInline[]): DocumentBlock[] {
  const trimmed = trimInlineEdges(inlines);
  return trimmed.some(inline => !inline.isLineBreak && (inline.text || '').trim()) ? [...blocks, { kind: 'paragraph', inlines: trimmed }] : blocks;
}
/** Flatten table cell blocks to spans. */
export function flattenBlocksToInlines(blocks: DocumentBlock[]): DocumentInline[] {
  let inlines: DocumentInline[] = [];
  for (const block of blocks) {
    if (inlines.length) inlines = appendInline(inlines, { isLineBreak: true });
    if (block.kind === 'paragraph' || block.kind === 'heading') for (const inline of block.inlines || []) inlines = appendInline(inlines, inline);
    else inlines = appendInline(inlines, { text: renderBlock(block, false) });
  }
  return inlines;
}
/** Keep only safe absolute links that Go's parser retains. */
export function safeLinkHref(value: string): string {
  const href = value.trim();
  if (href.length > 2048 || /[\s<>"]/u.test(href)) return '';
  return /^(https?:\/\/|mailto:)/i.test(href) ? href : '';
}
/** Keep a language token safe for a Markdown fence. */
export function markdownFenceLanguage(value: string): string { const language = value.trim().toLowerCase(); return language.length <= 32 && /^[a-z0-9+#._-]*$/.test(language) ? language : ''; }

/** Render format-neutral blocks as Confluence storage XHTML. */
export function renderStorageDocument(blocks: DocumentBlock[]): string { return blocks.map(renderStorageBlock).join(''); }
function renderStorageBlock(block: DocumentBlock): string {
  switch (block.kind) {
    case 'paragraph': return `<p>${renderStorageInlines(block.inlines || [])}</p>`;
    case 'heading': { const tag = 'h' + Math.min(6, Math.max(1, block.headingLevel || 1)); return `<${tag}>${renderStorageInlines(block.inlines || [])}</${tag}>`; }
    case 'bulletList': case 'orderedList': {
      const tag = block.kind === 'orderedList' ? 'ol' : 'ul';
      const start = tag === 'ol' && (block.orderedStart || 0) > 1 ? ` start="${block.orderedStart}"` : '';
      return `<${tag}${start}>${(block.listItems || []).map(item => `<li>${renderStorageListItem(item.blocks)}</li>`).join('')}</${tag}>`;
    }
    case 'taskList': return `<ac:task-list>${(block.listItems || []).map(item => `<ac:task><ac:task-status>${item.isTaskDone ? 'complete' : 'incomplete'}</ac:task-status><ac:task-body>${renderStorageListItem(item.blocks)}</ac:task-body></ac:task>`).join('')}</ac:task-list>`;
    case 'quote': return `<blockquote>${renderStorageDocument(block.children || [])}</blockquote>`;
    case 'code': return `<pre>${escapeStorageText(block.codeText || '')}</pre>`;
    case 'rule': return '<hr />';
    case 'table': return `<table><tbody>${(block.tableRows || []).map(row => `<tr>${row.cells.map(cell => `<${row.isHeader ? 'th' : 'td'}>${renderStorageInlines(cell)}</${row.isHeader ? 'th' : 'td'}>`).join('')}</tr>`).join('')}</tbody></table>`;
  }
}
function renderStorageListItem(blocks: DocumentBlock[]): string {
  return blocks.length && blocks[0]!.kind === 'paragraph' ? renderStorageInlines(blocks[0]!.inlines || []) + renderStorageDocument(blocks.slice(1)) : renderStorageDocument(blocks);
}
function renderStorageInlines(inlines: DocumentInline[]): string {
  return inlines.map(inline => {
    if (inline.isLineBreak) return '<br />';
    let opening = '', closing = '';
    if (inline.linkHref) { opening += `<a href="${escapeStorageAttribute(inline.linkHref)}">`; closing = '</a>' + closing; }
    for (const [isSet, open, close] of [
      [inline.isStrong, '<strong>', '</strong>'], [inline.isEmphasis, '<em>', '</em>'],
      [inline.isStrikethrough, '<span style="text-decoration: line-through;">', '</span>'], [inline.isCode, '<code>', '</code>'],
    ] as const) if (isSet) { opening += open; closing = close + closing; }
    return opening + escapeStorageText(inline.text || '') + closing;
  }).join('');
}
function escapeStorageText(value: string): string { return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); }
function escapeStorageAttribute(value: string): string { return escapeStorageText(value).replace(/"/g, '&quot;').replace(/'/g, '&#39;'); }
