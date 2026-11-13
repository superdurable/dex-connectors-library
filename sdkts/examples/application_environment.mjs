// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { inspect } from 'node:util';
import { ConfigurationStore, InMemoryObjectStore, LoadedProject } from '../dist/projectconfig/index.js';

// Dex Web's role: store the secret as an exact private version, then pin it in a configuration snapshot.
const scope = { projectId: 'example', kind: 'preview' };
const objects = new InMemoryObjectStore();
const configurations = new ConfigurationStore(objects, scope);
const declarations = [
  { name: 'REVIEW_API_KEY', required: true, secret: true, minLength: 8 },
  { name: 'SLACK_REVIEW_CHANNEL', required: true, enum: ['#ops', '#reviews'] },
];
const reviewKey = await configurations.createApplicationSecret('REVIEW_API_KEY', 'test-only-review-key');
const written = await configurations.writeConfiguration(0, {
  connections: [], triggerBindings: [], operationConfigurations: [],
  environment: { REVIEW_API_KEY: { secretRef: reviewKey }, SLACK_REVIEW_CHANNEL: { value: '#reviews' } },
});
await configurations.validateApplicationEnvironment(declarations, written);
const snapshot = await configurations.freezeConfiguration(written.revision);

// The Worker's role; a hosted Worker gets the same project from loadFromEnvironment().
const project = new LoadedProject(await configurations.readSnapshot(snapshot), snapshot, objects, undefined, undefined);
const environment = await project.resolveApplicationEnvironment();
const workerEnvironment = {};
environment.apply(workerEnvironment); // Omit the argument to set process.env before creating application clients.
if (workerEnvironment.REVIEW_API_KEY !== 'test-only-review-key' || workerEnvironment.SLACK_REVIEW_CHANNEL !== '#reviews') throw new Error('application environment example failed');
if (inspect(environment).includes('test-only') || String(environment).includes('test-only')) throw new Error('application environment was not redacted');
