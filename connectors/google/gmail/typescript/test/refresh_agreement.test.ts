// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { generateKeyPairSync } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError } from '@superdurable/connectors-sdk';
import { decodeGmailCredentials, encodeGmailCredentials } from '../src/credential_codec.js';
import { GmailCredentialRefreshDriver } from '../src/refresh.js';

interface AgreementCase {
  name: string;
  methodId: string;
  credentials: Record<string, string>;
  endpoint: string;
  encoding: string;
  parameters: Record<string, string>;
  response: Record<string, unknown>;
  updates: Record<string, string>;
  terminalErrorCodes: string[];
  keyCredential?: string;
  privateKeyMember?: string;
  keyMembers?: Record<string, string>;
}

const cases = JSON.parse(readFileSync(new URL('./fixtures/refresh-agreement-cases.json', import.meta.url), 'utf8')) as AgreementCase[];

describe('Go manifest refresh agreement', () => {
  for (const agreement of cases) it(agreement.name, async () => {
    const material: Record<string, string> = { ...agreement.credentials, auth_method: agreement.methodId };
    if (agreement.keyCredential && agreement.privateKeyMember) {
      const privateKey = generateKeyPairSync('rsa', { modulusLength: 2048 }).privateKey.export({ format: 'pem', type: 'pkcs8' }).toString();
      material[agreement.keyCredential] = JSON.stringify({ ...agreement.keyMembers, [agreement.privateKeyMember]: privateKey });
    }
    const credentials = decodeGmailCredentials(material);
    let response = agreement.response;
    let status = 200;
    const requests: { url: string; init: RequestInit }[] = [];
    const transport: typeof fetch = async (input, init) => {
      requests.push({ url: String(input), init: init ?? {} });
      return Response.json(response, { status });
    };
    const driver = new GmailCredentialRefreshDriver(transport);
    const state = { credentials, expiresAt: new Date(0), now: new Date() };
    const result = await driver.refresh(state, new AbortController().signal);
    expect(requests).toHaveLength(1);
    expect(requests[0]?.url).toBe(agreement.endpoint);
    const parameters = agreement.encoding === 'json'
      ? JSON.parse(String(requests[0]?.init.body)) as Record<string, string>
      : Object.fromEntries(new URLSearchParams(String(requests[0]?.init.body)));
    for (const [name, value] of Object.entries(agreement.parameters)) expect(parameters[name]).toBe(value);
    const expected = Object.fromEntries(Object.entries({ ...material, ...agreement.updates }).filter(([, value]) => Boolean(value)));
    expect(encodeGmailCredentials(result.credentials)).toEqual(expected);
    expect(result.expiresAt.getTime()).toBeGreaterThan(Date.now());

    for (const terminalCode of agreement.terminalErrorCodes) {
      requests.length = 0;
      response = { error: terminalCode };
      status = 400;
      await expect(driver.refresh(state, new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
      expect(requests).toHaveLength(1);
    }
  });
});
