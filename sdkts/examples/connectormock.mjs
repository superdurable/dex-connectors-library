// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import { branchCase, decodeOutput } from '../dist/connectormock/index.js';

const output = decodeOutput('{"id":"item-1"}', z.strictObject({ id: z.string() }));
const response = branchCase('created', output);
if (response.kind !== 'branch' || response.value.id !== 'item-1') throw new Error('mock example failed');
