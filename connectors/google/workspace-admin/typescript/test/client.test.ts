// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString } from '@superdurable/connectors-sdk';
import { WorkspaceAdminClient, goPathEscape, goQueryEncode } from '../src/client.js';

const credentials = { resolve: async () => ({ accessToken: new SecretString('token') }), resolveWithRefresh: async () => ({ accessToken: new SecretString('token') }) };

describe('Directory client inputs', () => {
  it('escapes paths and sorted queries like Go', () => {
    expect(goPathEscape("a!$&+,:;=@/?'()*~")).toBe('a%21$&+%2C:%3B=@%2F%3F%27%28%29%2A~');
    expect(goQueryEncode({ z: 'a b~*', a: 'x+y' })).toBe('a=x%2By&z=a+b%7E%2A');
  });
  it('rejects invalid limits before a provider call', () => {
    expect(() => new WorkspaceAdminClient({ maxResponseBytes: -1 }, credentials)).toThrow();
    expect(() => new WorkspaceAdminClient({ listUsersPageSize: 501 }, credentials)).toThrow();
  });
});
