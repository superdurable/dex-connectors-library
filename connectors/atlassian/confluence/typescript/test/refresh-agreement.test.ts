// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { Authorization, CredentialFields } from '../src/generated/refresh.js';

interface AgreementCase {
  name: string;
  expectedError?: 'retryable' | 'reauthorization';
  credentials: Record<string, string>;
  endpoint: string;
  encoding: string;
  clientAuth: string;
  parameters: Record<string, string>;
  response: Record<string, unknown>;
  updates: Record<string, string>;
  terminalErrorCodes?: string[];
}

const cases = JSON.parse(readFileSync(new URL('./fixtures/refresh-agreement-cases.json', import.meta.url), 'utf8')) as AgreementCase[];

function credentialValues(credentials: Record<string, unknown>): Record<string, string> {
  return Object.fromEntries(Object.entries(credentials).map(([name, value]) =>
    [name, value instanceof SecretString ? value.reveal() : String(value)]));
}

describe('Go manifest refresh agreement', () => {
  for (const agreement of cases) it(agreement.name, async () => {
    const credentials: Record<string, unknown> = {};
    for (const [name, value] of Object.entries(agreement.credentials)) {
      credentials[CredentialFields[name]!] = name === 'oauth_client_secret' || name.endsWith('_token')
        ? new SecretString(value) : value;
    }
    const initial = credentialValues(credentials);
    let response = agreement.response;
    let status = 200;
    const requests: Array<{ url: string; init: RequestInit }> = [];
    const transport: typeof fetch = async (input, init) => {
      requests.push({ url: String(input), init: init ?? {} });
      return Response.json(response, { status });
    };
    const driver = new ManifestCredentialRefreshDriver<Record<string, unknown>>({
      providerName: 'Confluence', auth: Authorization, credentialFields: CredentialFields, fetch: transport,
    });
    const state = { credentials, expiresAt: new Date(0), now: new Date() };
    let result: Awaited<ReturnType<typeof driver.refresh>> | undefined;
    try { result = await driver.refresh(state, new AbortController().signal); }
    catch (error) {
      expect(agreement.expectedError).toBeDefined();
      if (agreement.expectedError === 'reauthorization') expect(error).toBeInstanceOf(ReauthorizationRequiredError);
      else expect(error).not.toBeInstanceOf(ReauthorizationRequiredError);
    }
    expect(requests).toHaveLength(1);
    expect(requests[0]!.url).toBe(agreement.endpoint);
    expect(agreement.encoding).toBe('json');
    expect(agreement.clientAuth).toBe('client_secret_post');
    expect((requests[0]!.init.headers as Record<string, string>)['Content-Type']).toMatch(/^application\/json/);
    expect(JSON.parse(String(requests[0]!.init.body))).toEqual(agreement.parameters);
    if (agreement.expectedError) return;
    expect(result).toBeDefined();
    const expected = { ...initial };
    for (const [name, value] of Object.entries(agreement.updates)) expected[CredentialFields[name]!] = value;
    expect(credentialValues(result!.credentials)).toEqual(expected);
    expect(result!.expiresAt.getTime()).toBeGreaterThan(Date.now());
    for (const terminalCode of agreement.terminalErrorCodes ?? []) {
      requests.length = 0;
      response = { error: terminalCode };
      status = 400;
      await expect(driver.refresh(state, new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
      expect(requests).toHaveLength(1);
    }
  });
});
