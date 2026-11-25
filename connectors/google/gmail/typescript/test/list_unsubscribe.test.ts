// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';

import { validateListUnsubscribe } from '../src/message.js';

// Mirrors the Go connector's TestValidateListUnsubscribe table.
const cases: Array<{ name: string; targets: string[]; isOneClick?: boolean; isValid?: boolean }> = [
  { name: 'none', targets: [], isValid: true },
  { name: 'https and mailto', targets: ['https://example.com/unsubscribe?token=abc', 'mailto:news@example.com?subject=unsubscribe'], isValid: true },
  { name: 'one click with https', targets: ['https://example.com/u/1'], isOneClick: true, isValid: true },
  { name: 'upper-case scheme and port', targets: ['HTTPS://Example.com:8443/u'], isValid: true },
  { name: 'percent-encoded mailto', targets: ['mailto:news%40example.com'], isValid: true },
  { name: 'one click without https', targets: ['mailto:news@example.com'], isOneClick: true },
  { name: 'http', targets: ['http://example.com/u'] },
  { name: 'javascript', targets: ['javascript:alert(1)'] },
  { name: 'CRLF header injection', targets: ['https://example.com/u\r\nBcc: victim@example.com'] },
  { name: 'LF', targets: ['https://example.com/u\nX: y'] },
  { name: 'closing bracket adds an entry', targets: ['https://example.com/u>, <https://evil.example'] },
  { name: 'comma', targets: ['https://example.com/a,b'] },
  { name: 'space', targets: ['https://example.com/a b'] },
  { name: 'non-ASCII', targets: ['https://example.com/é'] },
  { name: 'https without host', targets: ['https:///path'] },
  { name: 'https without slashes', targets: ['https:example.com/u'] },
  { name: 'https with bad port', targets: ['https://example.com:80a/u'] },
  { name: 'https with underscore host', targets: ['https://exa_mple.com/u'] },
  { name: 'https with user info', targets: ['https://user:pass@example.com/u'] },
  { name: 'backslash', targets: ['https://example.com\\evil.com/u'] },
  { name: 'mailto without address', targets: ['mailto:'] },
  { name: 'mailto with invalid address', targets: ['mailto:not-an-address'] },
  { name: 'invalid percent escape', targets: ['mailto:%ZZ@example.com'] },
  { name: 'mailto encoded newline', targets: ['mailto:a@example.com%0D%0ABcc:b@example.com'] },
  { name: 'empty', targets: [''] },
  { name: 'duplicate', targets: ['https://example.com/u', 'https://example.com/u'] },
  { name: 'three targets', targets: ['https://example.com/1', 'https://example.com/2', 'mailto:a@example.com'] },
  { name: 'too long', targets: [`https://example.com/${'a'.repeat(512)}`] },
];

describe('validateListUnsubscribe', () => {
  for (const testCase of cases) {
    it(testCase.name, () => {
      const validate = () => validateListUnsubscribe(testCase.targets, testCase.isOneClick ?? false);
      if (testCase.isValid) expect(validate).not.toThrow();
      else expect(validate).toThrow(TypeError);
    });
  }
});
