// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { pathEscape, queryEscape } from '../src/client.js';
import { ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

// Recorded from Go net/url.QueryEscape and PathEscape.
it.each([
  ['a b~*!\'()', 'a+b~%2A%21%27%28%29', 'a%20b~%2A%21%27%28%29'],
  ['a/b;c,d?e', 'a%2Fb%3Bc%2Cd%3Fe', 'a%2Fb%3Bc%2Cd%3Fe'],
  ['é &+%', '%C3%A9+%26%2B%25', '%C3%A9%20&+%25'],
  ['$select=id,name', '%24select%3Did%2Cname', '$select=id%2Cname'],
  ["O'Neil", 'O%27Neil', 'O%27Neil'],
  ['b!team', 'b%21team', 'b%21team'],
])('uses Go URL escaping for %s', (value, query, path) => {
  expect(queryEscape(value)).toBe(query);
  expect(pathEscape(value)).toBe(path);
});

it('compares decoded request paths with Go URL.Path', () => {
  const scenario: Scenario = {
    name: 'decoded-path', operation: 'searchFiles', callId: 'test', input: {},
    exchanges: [{ method: 'GET', path: '/drives/teamDriveKey-2/items/folder-1:/Straße', keyMode: 'absent', status: 200 }],
    expected: { branch: 'found', failureKind: '', retry: false, retryAfterMs: 0, uncertain: false, requestCount: 1 },
  };
  const recorder = new ExchangeRecorder(scenario, '/v1.0');
  recorder.record({ method: 'GET', url: 'http://localhost/v1.0/drives/teamDriveKey-2/items/folder-1:/Stra%C3%9Fe' });
  expect(() => recorder.finish()).not.toThrow();
});
