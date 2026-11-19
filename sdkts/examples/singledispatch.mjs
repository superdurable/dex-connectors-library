// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { decideDispatch } from '../dist/singledispatch/index.js';

// The last heartbeat is opaque: pass Dex's value through. Without a checkpoint entry for this
// call ID, each earlier attempt counts as one possible send.
const callId = '5af8fafa-06ff-5b2d-bb13-04693809fabf';
const first = decideDispatch(callId, 1, undefined);
const later = decideDispatch(callId, 2, undefined);
if (first.kind !== 'sendAllowed' || later.kind !== 'readBackRequired') throw new Error('single-dispatch decision example failed');
const belowCap = decideDispatch(callId, 2, undefined, undefined, 3);
const atCap = decideDispatch(callId, 4, undefined, undefined, 3);
if (belowCap.kind !== 'readBackThenResendAllowed' || atCap.kind !== 'readBackRequired') throw new Error('bounded resend decision example failed');
