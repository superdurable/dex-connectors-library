// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

type ObjectValue = Record<string, unknown>;

/** Validated first-tab document used by read and guarded writes. */
export interface DocumentResource {
  /** Google document ID. */ readonly documentId: string;
  /** Google document title. */ readonly title: string;
  /** Current edit revision, empty for viewers. */ readonly revisionId: string;
  /** Whether the document has more than its first tab. */ readonly hasOtherTabs: boolean;
  /** First tab's stable ID. */ readonly tabId: string;
  /** First tab's structural content. */ readonly content: ObjectValue[];
  /** First tab's lists. */ readonly lists: ObjectValue;
  /** Exclusive end index of the first tab body. */ readonly bodyEndIndex: number;
  /** First text insertion index after a leading section break. */ readonly bodyStartIndex: number;
}

/** Validate the minimum Google Docs structure used by all operations. */
export function decodeDocument(body: Uint8Array): DocumentResource {
  const raw = parseObject(body);
  validateDocumentResource(raw);
  const documentId = stringField(raw, 'documentId');
  if (!isDriveID(documentId)) throw new TypeError('invalid document ID');
  const tabs = arrayField(raw, 'tabs');
  const first = objectValue(tabs[0]);
  const tabId = stringField(objectField(first, 'tabProperties'), 'tabId');
  if (!/^[A-Za-z0-9_.-]{1,256}$/.test(tabId)) throw new TypeError('invalid tab ID');
  const documentTab = objectField(first, 'documentTab');
  const content = arrayField(objectField(documentTab, 'body'), 'content').map(objectValue);
  const last = content.at(-1);
  if (!last || !isObject(last.paragraph) || !Number.isInteger(last.endIndex) || Number(last.endIndex) < 2) throw new TypeError('invalid last paragraph');
  const firstContent = content[0]!;
  const bodyStartIndex = Object.hasOwn(firstContent, 'sectionBreak') ? integerField(firstContent, 'endIndex', 0) : integerField(firstContent, 'startIndex', 0);
  const lists = documentTab.lists == null ? {} : objectValue(documentTab.lists);
  return {
    documentId, title: stringField(raw, 'title', ''), revisionId: stringField(raw, 'revisionId', ''),
    hasOtherTabs: tabs.length > 1 || (Array.isArray(first.childTabs) && first.childTabs.length > 0),
    tabId, content, lists, bodyEndIndex: Number(last.endIndex), bodyStartIndex,
  };
}

/** Join first-tab text runs, with non-text elements made nonmatching. */
export function documentBodyText(document: DocumentResource): string {
  return structuralText(document.content, 0);
}

function structuralText(content: ObjectValue[], depth: number): string {
  if (depth > 16) throw new TypeError('document tables are nested too deeply');
  let text = '';
  for (const element of content) {
    if (isObject(element.paragraph)) {
      for (const part of arrayField(element.paragraph, 'elements', [])) {
        const item = objectValue(part);
        text += isObject(item.textRun) ? stringField(item.textRun, 'content', '') : '\uE907';
      }
    } else if (isObject(element.table)) {
      for (const row of arrayField(element.table, 'tableRows', [])) {
        for (const cell of arrayField(objectValue(row), 'tableCells', [])) {
          text += structuralText(arrayField(objectValue(cell), 'content', []).map(objectValue), depth + 1);
        }
      }
    }
  }
  return text;
}

/** Render Google Docs paragraphs and tables as bounded plain text or Markdown. */
export function renderDocumentText(document: DocumentResource, format: 'markdown' | 'plainText'): string {
  const counters = new Map<string, number[]>();
  const blocks = renderContent(document.content, document.lists, format, counters, 0);
  return blocks.map((block, index) => index === 0 ? block.text : `${format === 'plainText' || blocks[index - 1]!.list && block.list ? '\n' : '\n\n'}${block.text}`).join('').replace(/^\n+|\n+$/g, '');
}

interface RenderedBlock { text: string; list: boolean }

function renderContent(content: ObjectValue[], lists: ObjectValue, format: 'markdown' | 'plainText', counters: Map<string, number[]>, depth: number): RenderedBlock[] {
  if (depth > 16) throw new TypeError('document tables are nested too deeply');
  const blocks: RenderedBlock[] = [];
  for (const element of content) {
    if (isObject(element.paragraph)) {
      const paragraph = element.paragraph;
      const { text, hasRule } = inlineText(paragraph, format);
      if (isObject(paragraph.bullet)) {
        const bullet = paragraph.bullet;
        const listId = stringField(bullet, 'listId', '');
        const level = Math.min(8, Math.max(0, integerField(bullet, 'nestingLevel', 0)));
        const list = isObject(lists[listId]) ? lists[listId] : {};
        const nesting = isObject(list) && isObject(list.listProperties) ? arrayField(list.listProperties, 'nestingLevels', []) : [];
        const glyph = level < nesting.length ? stringField(objectValue(nesting[level]), 'glyphType', '') : '';
        let marker = '- ';
        if (['DECIMAL', 'ZERO_DECIMAL', 'UPPER_ALPHA', 'ALPHA', 'UPPER_ROMAN', 'ROMAN'].includes(glyph)) {
          const counts = counters.get(listId) ?? [];
          while (counts.length <= level) counts.push(0);
          counts[level] = (counts[level] ?? 0) + 1;
          for (let index = level + 1; index < counts.length; index++) counts[index] = 0;
          counters.set(listId, counts);
          marker = `${counts[level]}. `;
        }
        blocks.push({ text: `${(format === 'markdown' ? '    ' : '  ').repeat(level)}${marker}${text.replace(/\n/g, ' ')}`, list: true });
        continue;
      }
      const style = isObject(paragraph.paragraphStyle) ? stringField(paragraph.paragraphStyle, 'namedStyleType', '') : '';
      const heading = ({ TITLE: 1, SUBTITLE: 2, HEADING_1: 1, HEADING_2: 2, HEADING_3: 3, HEADING_4: 4, HEADING_5: 5, HEADING_6: 6 } as Record<string, number>)[style];
      if (heading && goHasNonSpace(text)) blocks.push({ text: format === 'markdown' ? `${'#'.repeat(heading)} ${text.replace(/\n/g, ' ')}` : text.replace(/\n/g, ' '), list: false });
      else if (!goHasNonSpace(text)) {
        if (hasRule && format === 'markdown') blocks.push({ text: '---', list: false });
        else if (format === 'plainText') blocks.push({ text: '', list: false });
      } else {
        blocks.push({ text: format === 'markdown' ? escapeMarkdownLineStarts(text) : text, list: false });
      }
    } else if (isObject(element.table)) {
      const rows = arrayField(element.table, 'tableRows', []).map(row => arrayField(objectValue(row), 'tableCells', []).map(cell => cellText(arrayField(objectValue(cell), 'content', []).map(objectValue), format, 1)));
      const columns = Math.max(0, ...rows.map(row => row.length));
      if (columns) {
        const lines: string[] = [];
        rows.forEach((row, index) => {
          while (row.length < columns) row.push('');
          if (format === 'plainText') lines.push(row.join('\t'));
          else { lines.push(`| ${row.join(' | ')} |`); if (index === 0) lines.push(`|${' --- |'.repeat(columns)}`); }
        });
        blocks.push({ text: lines.join('\n'), list: false });
      }
    }
  }
  return blocks;
}

function cellText(content: ObjectValue[], format: 'markdown' | 'plainText', depth: number): string {
  if (depth > 16) throw new TypeError('document tables are nested too deeply');
  const parts: string[] = [];
  for (const element of content) {
    if (isObject(element.paragraph)) {
      const { text } = inlineText(element.paragraph, format);
      if (goHasNonSpace(text)) parts.push(text);
    } else if (isObject(element.table)) {
      for (const row of arrayField(element.table, 'tableRows', [])) for (const cell of arrayField(objectValue(row), 'tableCells', [])) {
        const nested = cellText(arrayField(objectValue(cell), 'content', []).map(objectValue), format, depth + 1);
        if (nested) parts.push(nested);
      }
    }
  }
  const joined = parts.join(' ').replace(/[\n\t]/g, ' ');
  return format === 'markdown' ? joined.replace(/\|/g, '\\|') : joined;
}

function inlineText(paragraph: ObjectValue, format: 'markdown' | 'plainText'): { text: string; hasRule: boolean } {
  let text = '';
  let hasRule = false;
  for (const part of arrayField(paragraph, 'elements', [])) {
    const value = objectValue(part);
    if (isObject(value.textRun)) text += stringField(value.textRun, 'content', '').replace(/\uE907/g, '').replace(/\v/g, '\n');
    else if (isObject(value.person)) {
      const properties = isObject(value.person.personProperties) ? value.person.personProperties : {};
      text += stringField(properties, 'name', '') || stringField(properties, 'email', '');
    } else if (isObject(value.richLink)) {
      const properties = isObject(value.richLink.richLinkProperties) ? value.richLink.richLinkProperties : {};
      const uri = stringField(properties, 'uri', '');
      const label = stringField(properties, 'title', '') || uri;
      text += format === 'markdown' && uri ? `[${label.replace(/\\/g, '\\\\').replace(/\[/g, '\\[').replace(/\]/g, '\\]')}](${uri.replace(/ /g, '%20').replace(/\(/g, '%28').replace(/\)/g, '%29').replace(/</g, '%3C').replace(/>/g, '%3E')})` : label;
    } else if (isObject(value.dateElement)) text += stringField(isObject(value.dateElement.dateElementProperties) ? value.dateElement.dateElementProperties : {}, 'displayText', '');
    else if (isObject(value.footnoteReference)) text += `${format === 'markdown' ? '[^' : '['}${stringField(value.footnoteReference, 'footnoteNumber', '')}]`;
    else if (Object.hasOwn(value, 'horizontalRule')) hasRule = true;
  }
  return { text: text.endsWith('\n') ? text.slice(0, -1) : text, hasRule };
}

function escapeMarkdownLineStarts(value: string): string {
  return value.split('\n').map(line => /^\d{1,9}[.)](?:[ \t\f\r]|$)/.test(line) ? line.replace(/^(\d{1,9})([.)])/, '$1\\$2') : /^(?:#{1,6}(?:[ \t\f\r]|$)|[-+*](?:[ \t\f\r]|$)|>|```|~~~|(?:-[ \t\f\r]*){3,}$|(?:\*[ \t\f\r]*){3,}$|(?:_[ \t\f\r]*){3,}$|=+[ \t\f\r]*$)/.test(line) ? `\\${line}` : line).join('\n');
}

/** Parse a provider JSON object without string coercion. */
export function parseObject(body: Uint8Array): ObjectValue {
  return objectValue(JSON.parse(Buffer.from(body).toString('utf8')) as unknown);
}

/** Require an object-shaped provider value. */
export function objectValue(value: unknown): ObjectValue {
  if (!isObject(value)) throw new TypeError('provider object is invalid');
  return value;
}

/** Require one object field. */
export function objectField(value: ObjectValue, name: string): ObjectValue { return objectValue(value[name]); }

/** Require one string field, optionally using a Go zero value. */
export function stringField(value: ObjectValue, name: string, fallback?: string): string {
  const field = value[name];
  if (field == null && fallback !== undefined) return fallback;
  if (typeof field !== 'string') throw new TypeError(`provider ${name} is invalid`);
  return field;
}

/** Require one integer field, optionally using a Go zero value. */
export function integerField(value: ObjectValue, name: string, fallback?: number): number {
  const field = value[name];
  if (field == null && fallback !== undefined) return fallback;
  if (!Number.isSafeInteger(field)) throw new TypeError(`provider ${name} is invalid`);
  return field as number;
}

/** Require one array field, optionally using an empty Go slice. */
export function arrayField(value: ObjectValue, name: string, fallback?: unknown[]): unknown[] {
  const field = name ? value[name] : value;
  if (field == null && fallback !== undefined) return fallback;
  if (!Array.isArray(field)) throw new TypeError(`provider ${name} is invalid`);
  return field;
}

/** Match the IDs accepted by the Go connector. */
export function isDriveID(value: string): boolean { return /^[A-Za-z0-9_-]{1,256}$/.test(value); }

/** Match Go strings.TrimSpace when checking whether text is blank. */
export function goHasNonSpace(value: string): boolean {
  return /[^\p{White_Space}]/u.test(value);
}

function isObject(value: unknown): value is ObjectValue { return !!value && typeof value === 'object' && !Array.isArray(value); }

function validateDocumentResource(document: ObjectValue): void {
  checkString(document, 'documentId'); checkString(document, 'title'); checkString(document, 'revisionId');
  const tabs = checkArray(document, 'tabs');
  tabs?.forEach(tabValue => validateTab(objectValue(tabValue)));
}

function validateTab(tab: ObjectValue): void {
  const properties = checkObject(tab, 'tabProperties');
  if (properties) checkString(properties, 'tabId');
  checkArray(tab, 'childTabs')?.forEach(child => validateTab(objectValue(child)));
  const documentTab = checkObject(tab, 'documentTab');
  if (!documentTab) return;
  const body = checkObject(documentTab, 'body');
  if (body) checkArray(body, 'content')?.forEach(value => validateElement(objectValue(value), 0));
  const lists = checkObject(documentTab, 'lists');
  if (lists) for (const value of Object.values(lists)) {
    if (value == null) continue;
    const list = objectValue(value);
    const listProperties = checkObject(list, 'listProperties');
    if (listProperties) checkArray(listProperties, 'nestingLevels')?.forEach(level => {
      const item = objectValue(level); checkString(item, 'glyphType'); checkString(item, 'glyphSymbol');
    });
  }
}

function validateElement(element: ObjectValue, depth: number): void {
  if (depth > 16) throw new TypeError('document tables are nested too deeply');
  checkInteger(element, 'startIndex'); checkInteger(element, 'endIndex');
  const paragraph = checkObject(element, 'paragraph');
  if (paragraph) {
    const style = checkObject(paragraph, 'paragraphStyle');
    if (style) checkString(style, 'namedStyleType');
    const bullet = checkObject(paragraph, 'bullet');
    if (bullet) { checkString(bullet, 'listId'); checkInteger(bullet, 'nestingLevel'); }
    checkArray(paragraph, 'elements')?.forEach(value => {
      const item = objectValue(value);
      const textRun = checkObject(item, 'textRun'); if (textRun) checkString(textRun, 'content');
      const footnote = checkObject(item, 'footnoteReference'); if (footnote) checkString(footnote, 'footnoteNumber');
      const person = checkObject(item, 'person');
      if (person) { const properties = checkObject(person, 'personProperties'); if (properties) { checkString(properties, 'name'); checkString(properties, 'email'); } }
      const link = checkObject(item, 'richLink');
      if (link) { const properties = checkObject(link, 'richLinkProperties'); if (properties) { checkString(properties, 'title'); checkString(properties, 'uri'); } }
      const date = checkObject(item, 'dateElement');
      if (date) { const properties = checkObject(date, 'dateElementProperties'); if (properties) checkString(properties, 'displayText'); }
    });
  }
  const table = checkObject(element, 'table');
  if (table) checkArray(table, 'tableRows')?.forEach(rowValue => {
    const row = objectValue(rowValue);
    checkArray(row, 'tableCells')?.forEach(cellValue => {
      const cell = objectValue(cellValue);
      checkArray(cell, 'content')?.forEach(value => validateElement(objectValue(value), depth + 1));
    });
  });
  const toc = checkObject(element, 'tableOfContents');
  if (toc) checkArray(toc, 'content')?.forEach(value => validateElement(objectValue(value), depth + 1));
}

function checkObject(value: ObjectValue, field: string): ObjectValue | undefined {
  return value[field] == null ? undefined : objectValue(value[field]);
}

function checkArray(value: ObjectValue, field: string): unknown[] | undefined {
  const item = value[field];
  if (item == null) return undefined;
  if (!Array.isArray(item)) throw new TypeError(`provider ${field} is invalid`);
  return item;
}

function checkString(value: ObjectValue, field: string): void {
  if (value[field] != null && typeof value[field] !== 'string') throw new TypeError(`provider ${field} is invalid`);
}

function checkInteger(value: ObjectValue, field: string): void {
  if (value[field] != null && !Number.isSafeInteger(value[field])) throw new TypeError(`provider ${field} is invalid`);
}
