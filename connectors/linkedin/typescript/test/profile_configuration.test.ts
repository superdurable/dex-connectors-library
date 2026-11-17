// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { parse } from 'yaml';
import { LinkedInClient } from '../src/index.js';
import { ProfileChangeFlow } from '../examples/profile-changed/flow.js';

const manifest = parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as {
  spec: { configuration: { fields: Array<{ name: string; description: string; default?: unknown }> }; auth: { guide: { startURL: string; steps: string[] }; fields: Array<{ name: string; description: string; type: string }> }; triggers: Array<{ name: string }> };
};

it('documents every visible connection field and keeps the Trigger binding fieldless', () => {
  expect(manifest.spec.configuration.fields.map(field => field.name)).toEqual(['userInfoUrl', 'timeout', 'maxResponseBytes', 'pollInterval']);
  for (const field of manifest.spec.configuration.fields) {
    expect(field.default).toBeDefined();
    expect(field.description.length).toBeGreaterThan(40);
    expect(field.description).toMatch(/blank|only to change/i);
  }
  expect(manifest.spec.auth.guide.startURL).toBe('https://www.linkedin.com/developers/apps');
  expect(manifest.spec.auth.guide.steps.join(' ')).toMatch(/redirect uri/i);
  expect(manifest.spec.auth.guide.steps.join(' ')).toMatch(/openid.*profile.*email/i);
  expect(manifest.spec.auth.fields.map(field => field.name)).toEqual(['oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token']);
  for (const field of manifest.spec.auth.fields) expect(field.description.length).toBeGreaterThan(40);
  expect(manifest.spec.triggers.map(trigger => trigger.name)).toContain('profileChanged');
  expect(new ProfileChangeFlow().getConnectorTriggerBindings()[0]?.configurationUI?.units ?? []).toEqual([]);
});

it('uses the manifest poll default and rejects intervals below one minute', () => {
  const reference = { provider: 'linkedin', name: 'watcher' };
  const credentials = new StaticCredentialProvider([[reference, { accessToken: new SecretString('token') }]]);
  expect(new LinkedInClient({}, credentials).pollIntervalMs).toBe(900_000);
  expect(new LinkedInClient({ pollIntervalMs: 60_000 }, credentials).pollIntervalMs).toBe(60_000);
  expect(() => new LinkedInClient({ pollIntervalMs: 59_999 }, credentials)).toThrow('at least one minute');
});
