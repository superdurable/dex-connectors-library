// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { convertWrittenBody } from '../src/markdown.js';
import { parseStorageDocument } from '../src/storage.js';
import { parseADFDocument } from '../src/adf.js';
import { documentComparisonText, renderDocumentText } from '../src/document.js';

function markdownToStorage(markdown: string): string { return convertWrittenBody(markdown, 'markdown', 'body').storage; }
function storageToText(storage: string, format: string): string { return renderDocumentText(parseStorageDocument(storage), format, 0).text; }

describe('Go document conversion tables', () => {
  it('renders escaped Markdown to Confluence storage', () => {
    const markdown = [
      '# Remote work', '',
      'Staff may work **remotely** two *days* a week, see [the handbook](https://example.com/handbook?a=1&b=2).',
      'Use `vpn connect` and ~~fax~~ email.', '',
      '- Ask your manager', '  - in writing', '- Log the days', '',
      '3. third', '4. fourth', '',
      '- [x] signed', '- [ ] filed', '',
      '> Approved by HR', '',
      '```bash', "echo '<b>' && exit", '```', '',
      '| Role | Days |', '| --- | ---: |', '| Staff | 2 |', '',
      '---', '',
      'Plain <script>alert(1)</script> & snake_case_name, [bad](javascript:alert(1)).',
    ].join('\n');
    expect(markdownToStorage(markdown)).toBe('<h1>Remote work</h1>' +
      '<p>Staff may work <strong>remotely</strong> two <em>days</em> a week, see <a href="https://example.com/handbook?a=1&amp;b=2">the handbook</a>.<br />' +
      'Use <code>vpn connect</code> and <span style="text-decoration: line-through;">fax</span> email.</p>' +
      '<ul><li>Ask your manager<ul><li>in writing</li></ul></li><li>Log the days</li></ul>' +
      '<ol start="3"><li>third</li><li>fourth</li></ol>' +
      '<ac:task-list><ac:task><ac:task-status>complete</ac:task-status><ac:task-body>signed</ac:task-body></ac:task>' +
      '<ac:task><ac:task-status>incomplete</ac:task-status><ac:task-body>filed</ac:task-body></ac:task></ac:task-list>' +
      '<blockquote><p>Approved by HR</p></blockquote>' +
      '<pre>echo \'&lt;b&gt;\' &amp;&amp; exit</pre>' +
      '<table><tbody><tr><th>Role</th><th>Days</th></tr><tr><td>Staff</td><td>2</td></tr></tbody></table>' +
      '<hr />' +
      '<p>Plain &lt;script&gt;alert(1)&lt;/script&gt; &amp; snake_case_name, bad.</p>');
  });
  it('keeps plain text as text', () => {
    expect(convertWrittenBody('\r\n# Not a heading\r\n**not bold** <b>\n  \n\n\nNew paragraph\n', 'plainText', 'body').storage)
      .toBe('<p># Not a heading<br />**not bold** &lt;b&gt;</p><p>New paragraph</p>');
  });
  it.each([
    ['  ', 'markdown', 'body is required'],
    ['bell\x07', 'markdown', 'body cannot contain control characters'],
    ['a'.repeat(262145), 'plainText', 'body cannot exceed 262144 characters'],
    ['text', 'html', 'bodyFormat must be markdown or plainText'],
  ])('validates written text: %s', (body, format, message) => {
    expect(() => convertWrittenBody(body, format, 'body')).toThrow(message);
  });
  it('reads storage Markdown and plain text', () => {
    expect(storageToText('<h1>Title</h1><p>One <em>two</em><br />three</p><ul><li>item</li></ul><pre>code  kept</pre>', 'plainText'))
      .toBe('Title\n\nOne two\nthree\n\n- item\n\ncode  kept');
    expect(storageToText('<p>a*b* [x] snake_case _lead # not</p><p># hash</p><p>- dash</p>', 'markdown'))
      .toBe('a\\*b\\* \\[x\\] snake_case \\_lead # not\n\n\\# hash\n\n\\- dash');
  });
  it('reads Confluence storage macros, lists, tables, and links as Markdown', () => {
    const storage = '<h2>Scope&nbsp;and   intent</h2>' +
      '<p>Applies to <strong>all</strong> staff.<br/>See <a href="https://example.com/x">policy</a> and ' +
      '<ac:link><ri:page ri:content-title="Leave policy" /><ac:plain-text-link-body><![CDATA[leave]]></ac:plain-text-link-body></ac:link>.</p>' +
      '<ac:structured-macro ac:name="info" ac:schema-version="1"><ac:rich-text-body><p>Reviewed yearly.</p></ac:rich-text-body></ac:structured-macro>' +
      '<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[if a < b {\n}]]></ac:plain-text-body></ac:structured-macro>' +
      '<ul><li><p>First</p><ol><li>nested</li></ol></li><li>Second <ac:structured-macro ac:name="status"><ac:parameter ac:name="title">DONE</ac:parameter></ac:structured-macro></li></ul>' +
      '<ac:task-list><ac:task><ac:task-id>1</ac:task-id><ac:task-status>complete</ac:task-status><ac:task-body>Sign</ac:task-body></ac:task></ac:task-list>' +
      '<table><tbody><tr><th>Role</th><th>Days</th></tr><tr><td><p>Staff | team</p></td><td>2</td></tr></tbody></table>' +
      '<ac:structured-macro ac:name="toc"/><p><ac:image><ri:attachment ri:filename="x.png"/></ac:image>1. not a list</p>';
    expect(storageToText(storage, 'markdown')).toBe([
      '## Scope and intent', '', 'Applies to **all** staff.', 'See [policy](https://example.com/x) and leave.', '',
      '> Reviewed yearly.', '', '```go', 'if a < b {', '}', '```', '',
      '- First', '', '  1. nested', '- Second DONE', '', '- [x] Sign', '',
      '| Role | Days |', '| --- | --- |', '| Staff \\| team | 2 |', '', '1\\. not a list',
    ].join('\n'));
  });
  it('round trips Markdown through storage', () => {
    const markdown = ['# Remote work', '', 'Staff may work **remotely** two *days* a week.', 'Second line with `code` and [a link](https://example.com).', '', '- Ask your manager', '- Log the days', '', '1. first', '2. second', '', '> Approved', '', '```', 'x := 1', '```', '', '---'].join('\n');
    expect(storageToText(markdownToStorage(markdown), 'markdown')).toBe(markdown);
    expect(markdownToStorage('```go\nx := 1\n```')).toBe('<pre>x := 1</pre>');
  });
  it('parses ADF and rejects a non-document root', () => {
    const adf = JSON.stringify({ type: 'doc', version: 1, content: [
      { type: 'heading', attrs: { level: 3 }, content: [{ type: 'text', text: 'Impact' }] },
      { type: 'paragraph', content: [{ type: 'text', text: 'Bold', marks: [{ type: 'strong' }] }, { type: 'text', text: ' and ' }, { type: 'text', text: 'site', marks: [{ type: 'link', attrs: { href: 'https://example.com' } }] }, { type: 'hardBreak' }, { type: 'mention', attrs: { text: '@Ada' } }, { type: 'text', text: ' ' }, { type: 'date', attrs: { timestamp: '1790726400000' } }] },
      { type: 'orderedList', attrs: { order: 2 }, content: [{ type: 'listItem', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'two' }] }] }] },
      { type: 'taskList', content: [{ type: 'taskItem', attrs: { state: 'DONE' }, content: [{ type: 'text', text: 'done' }] }] },
      { type: 'panel', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Note' }] }] },
      { type: 'codeBlock', attrs: { language: 'sql' }, content: [{ type: 'text', text: 'select 1' }] },
      { type: 'table', content: [{ type: 'tableRow', content: [{ type: 'tableHeader', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'A' }] }] }] }, { type: 'tableRow', content: [{ type: 'tableCell', content: [{ type: 'paragraph', content: [{ type: 'text', text: '1' }] }] }] }] },
      { type: 'mediaSingle', content: [{ type: 'media', attrs: { id: 'x' } }] }, { type: 'rule' },
    ] });
    expect(renderDocumentText(parseADFDocument(adf), 'markdown', 0).text).toBe(['### Impact', '', '**Bold** and [site](https://example.com)', '@Ada 2026-09-30', '', '2. two', '', '- [x] done', '', '> Note', '', '```sql', 'select 1', '```', '', '| A |', '| --- |', '| 1 |', '', '---'].join('\n'));
    expect(() => parseADFDocument('{"type":"paragraph"}')).toThrow();
  });
  it.each([
    ['+123', '1970-01-01'],
    ['8640000000000000', '275760-09-13'],
    ['9223372036854775807', '292278994-08-17'],
    ['-9223372036854775808', '-292275055-05-16'],
  ])('formats Go int64 ADF date %s', (timestamp, expected) => {
    const adf = JSON.stringify({ type: 'doc', version: 1, content: [{ type: 'paragraph', content: [{ type: 'date', attrs: { timestamp } }] }] });
    expect(renderDocumentText(parseADFDocument(adf), 'plainText', 0).text).toBe(expected);
  });
  it('truncates by Unicode character and compares normalized storage', () => {
    expect(renderDocumentText(parseStorageDocument('<p>héllo wörld</p>'), 'plainText', 4)).toEqual({ text: 'héll', isTruncated: true });
    const ours = markdownToStorage('# Title\n\nOne **two**\n\n- item');
    const normalized = '<h1 local-id="a1">Title</h1>\n<p local-id="b2">One  <strong>two</strong></p><ul><li><p>item</p></li></ul>';
    expect(documentComparisonText(parseStorageDocument(ours))).toBe(documentComparisonText(parseStorageDocument(normalized)));
  });
  it('bounds hostile storage and accepts unclosed ordinary tags', () => {
    const deep = '<div>'.repeat(5000) + 'deep text' + '</div>'.repeat(5000);
    expect(() => renderDocumentText(parseStorageDocument(deep), 'markdown', 0)).not.toThrow();
    expect(() => parseStorageDocument('<p>unterminated <![CDATA[ x')).toThrow();
    expect(storageToText('<p>open <strong>bold', 'markdown')).toBe('open **bold**');
  });
});
