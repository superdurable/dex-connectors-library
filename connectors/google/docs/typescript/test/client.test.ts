// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { DocsClient, DocsRequestError, goPathEscape, goQuery } from '../src/client.js';
import { decodeDocument, renderDocumentText } from '../src/document.js';

const credentials = new StaticCredentialProvider([[{ provider: 'google', name: 'policy-docs' }, { accessToken: new SecretString('safe-token') }]]);

describe('Google Docs client bounds and wire format', () => {
  it('matches Go path and query escaping', () => {
    expect(goPathEscape("a!$&'()+,/:;=?@~")).toBe('a%21$&%27%28%29+%2C%2F:%3B%3D%3F@~'.replace('%3D', '='));
    expect(goQuery({ z: 'a b~*', a: 'x/y' })).toBe('a=x%2Fy&z=a+b%7E%2A');
  });

  it('rejects unsafe endpoint and limit configuration at construction', () => {
    expect(() => new DocsClient({ docsEndpoint: 'http://example.com' }, credentials)).toThrow();
    expect(() => new DocsClient({ maxResponseBytes: 0 }, credentials)).toThrow();
    expect(() => new DocsClient({ maxTextBytes: 4_194_305 }, credentials)).toThrow();
  });

  it('renders a heading and list without exposing extra tab content', () => {
    const document = decodeDocument(Buffer.from(JSON.stringify({ documentId: 'doc1', title: 'Policy', revisionId: 'rev1', tabs: [
      { tabProperties: { tabId: 't.0' }, documentTab: { body: { content: [
        { endIndex: 1, sectionBreak: {} },
        { startIndex: 1, endIndex: 7, paragraph: { paragraphStyle: { namedStyleType: 'HEADING_1' }, elements: [{ textRun: { content: 'Policy\n' } }] } },
        { startIndex: 7, endIndex: 15, paragraph: { bullet: { listId: 'l1', nestingLevel: 0 }, elements: [{ textRun: { content: 'Approve\n' } }] } },
      ] }, lists: { l1: { listProperties: { nestingLevels: [{ glyphType: 'DECIMAL' }] } } } } },
      { tabProperties: { tabId: 't.1' }, documentTab: { body: { content: [] } } },
    ] })));
    expect(document.hasOtherTabs).toBe(true);
    expect(renderDocumentText(document, 'markdown')).toBe('# Policy\n\n1. Approve');
  });

  it('rejects wrongly typed provider identity fields', () => {
    expect(() => decodeDocument(Buffer.from('{"documentId":42,"tabs":[]}'))).toThrow();
    expect(() => decodeDocument(Buffer.from('{"documentId":"doc1","tabs":[{"tabProperties":{"tabId":"t.0"},"documentTab":{"body":{"content":[{"table":"wrong type"},{"endIndex":2,"paragraph":{}}]}}}]}'))).toThrow();
  });

  it('classifies an oversized error body by status and bounds successful bodies', async () => {
    const call = { context: { cancellationSignal: new AbortController().signal } } as Call;
    const credential = { accessToken: new SecretString('safe-token') };
    const errorClient = new DocsClient({ maxResponseBytes: 4 }, credentials, { transport: async () => ({ status: 429, headers: {}, body: Buffer.alloc(65_537, 65) }) });
    const error = await errorClient.request(call, credential, 'GET', 'https://docs.googleapis.com/v1/documents/doc1');
    expect(error.status).toBe(429);
    expect(error.body.byteLength).toBe(0);
    const successClient = new DocsClient({ maxResponseBytes: 4 }, credentials, { transport: async () => ({ status: 200, headers: {}, body: Buffer.from('long body') }) });
    await expect(successClient.request(call, credential, 'GET', 'https://docs.googleapis.com/v1/documents/doc1')).rejects.toBeInstanceOf(DocsRequestError);
  });
});
