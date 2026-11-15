// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { decideDispatch } from '../dist/singledispatch/index.js';

const callId = '5af8fafa-06ff-5b2d-bb13-04693809fabf';
const first = decideDispatch(callId, 1, undefined);
const later = decideDispatch(callId, 2, { callId, attempt: 1, state: 'claimed' });
if (first.kind !== 'sendAllowed' || later.kind !== 'readBackRequired') throw new Error('single-dispatch decision example failed');
const belowCap = decideDispatch(callId, 2, { callId, attempt: 1, state: 'claimed', sendCount: 1 }, undefined, 3);
const atCap = decideDispatch(callId, 4, { callId, attempt: 3, state: 'claimed', sendCount: 3 }, undefined, 3);
if (belowCap.kind !== 'readBackThenResendAllowed' || atCap.kind !== 'readBackRequired') throw new Error('bounded resend decision example failed');
