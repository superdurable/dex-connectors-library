// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { WeilingClient } from '../src/client.js';

it('shares an authorization request and refreshes at the safety window or credential rotation', async () => {
  let milliseconds = 0;
  let issued = 0;
  const client = new WeilingClient({}, { resolve: async () => ({appId: new SecretString('account'), appSecret: new SecretString('unused')}) }, {
    now: () => new Date(milliseconds),
    send: async request => {
      expect(new URL(request.url).pathname).toEqual('/openapi/auth/access_token/get');
      const token = 'token-' + ++issued;
      return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({code: 0, data: {access_token: token, expires_in: 180}})) };
    },
  });
  const call = {context: {cancellationSignal: new AbortController().signal}} as Call;
  const token = (secret: string) => client.accessToken(call, 'account', secret);
  expect(await Promise.all([token('first'), token('first')])).toEqual(['token-1', 'token-1']);
  milliseconds = 59_999;
  expect(await token('first')).toEqual('token-1');
  milliseconds = 60_000;
  expect(await token('first')).toEqual('token-2');
  expect(await token('rotated')).toEqual('token-3');
  expect(issued).toBe(3);
});
