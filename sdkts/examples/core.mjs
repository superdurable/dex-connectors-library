// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { CallIDSchema, ReceiptSchema, newQueryBranch, defaultConnectorStepExecuteRetryPolicy, SecretString } from '../dist/index.js';

const callId = CallIDSchema.parse('6750e62f-4d15-4e7e-a9d3-ec90f4f17b35');
const receipt = ReceiptSchema.parse({ callId, provider: 'fixture', observedAt: new Date().toISOString() });
const result = newQueryBranch('found', { title: 'Example' }, receipt);
const retryPolicy = defaultConnectorStepExecuteRetryPolicy();
const credential = new SecretString('test-only');
if (result.kind !== 'branch' || retryPolicy.totalDurationMs !== 3600000 || credential.reveal() !== 'test-only') throw new Error('core example failed');
