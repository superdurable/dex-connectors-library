// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import { branchCase, decodeOutput, newMutation } from '../dist/connectormock/index.js';
import { createCall, runMutation } from '../dist/index.js';

const output = decodeOutput('{"id":"item-1"}', z.strictObject({ id: z.string() }));
const response = branchCase('created', output);
if (response.kind !== 'branch' || response.value.id !== 'item-1') throw new Error('mock example failed');

const definition = { operation: { connectorId: 'sample', operationId: 'createItem' }, branches: [{ id: 'created', description: 'Created' }, { id: 'defect', description: 'Connector defect', optional: true }], stepDefaults: {} };
const mutation = newMutation(definition).respond(branchCase('created', output));
const context = { flowId: 'example-flow', stepExecutionId: 'createItem:1', attempt: 1 }; // Test context; a real Step receives Dex AsyncContext.
const call = createCall(context, { provider: 'sample', name: 'default' }, definition.operation);
const result = await runMutation(mutation, call, { id: 'item-1' });
if (result.branch !== 'created' || mutation.calls().length !== 1) throw new Error('scripted mock example failed');
mutation.assertNoFailures();
