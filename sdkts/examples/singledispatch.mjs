// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { decideDispatch } from '../dist/singledispatch/index.js';

const callId = '5af8fafa-06ff-5b2d-bb13-04693809fabf';
const first = decideDispatch(callId, 1, undefined);
const later = decideDispatch(callId, 2, { callId, attempt: 1, state: 'claimed' });
if (first.kind !== 'sendAllowed' || later.kind !== 'readBackRequired') throw new Error('single-dispatch decision example failed');
