// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { expect, it } from 'vitest';
import YAML from 'yaml';
import { LinearIssueEventIntakeFlow, ConnectionName, IssueCreatedBinding } from '../examples/issue-event-intake/flow.js';

const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8'));

it('documents every visible connection and Trigger field in Dex Web', () => {
  const fields = [...manifest.spec.configuration.fields, ...manifest.spec.auth.methods.flatMap((method: { fields: Array<{ description: string }> }) => method.fields)];
  expect(fields.map((field: { name: string }) => field.name)).toContain('webhook_signing_secret');
  for (const field of fields) {
    expect(field.description.length).toBeGreaterThan(80);
    expect(field.description).toMatch(/blank|Blank|Required|never enter/);
  }
  for (const method of manifest.spec.auth.methods) {
    expect(method.guide.startURL).toMatch(/^https:\/\/linear\.app\//);
    expect(method.guide.steps.length).toBeGreaterThan(2);
  }
  const binding = new LinearIssueEventIntakeFlow().getConnectorTriggerBindings()[0]!;
  expect(binding.definition.trigger).toEqual({ connectorId: 'linear', triggerName: 'issueEventReceived' });
  expect(binding.connectionName).toBe(ConnectionName);
  expect(binding.bindingName).toBe(IssueCreatedBinding);
  expect(binding.configurationUI?.units).toHaveLength(1);
  const unit = binding.configurationUI!.units[0]!;
  expect(unit.unitId).toBe('teamPicker');
  expect(unit.bindings).toEqual([{ port: 'teamId', jsonPointer: '/teamId' }]);
  expect(unit.description).toContain('https://linear.app/settings/teams');
  expect(unit.description).toMatch(/Leave blank/);
  expect(manifest.spec.studio.units[0].description).toContain('team UUID');
});
