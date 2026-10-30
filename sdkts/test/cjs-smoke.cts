// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString } from '@superdurable/connectors-sdk';
import { parseRetryAfter } from '@superdurable/connectors-sdk/providerhttp';
import { scopePrefix } from '@superdurable/connectors-sdk/projectconfig';
import { branchCase } from '@superdurable/connectors-sdk/connectormock';

const secret = new SecretString('test-only');
const delay: number = parseRetryAfter('7');
const prefix: string = scopePrefix({ projectId: 'sample', kind: 'preview' });
const outcome = branchCase('done', { count: 1 });
if (secret.reveal() !== 'test-only' || delay !== 7000 || !prefix || outcome.kind !== 'branch') throw new Error('CJS declaration smoke test failed');
