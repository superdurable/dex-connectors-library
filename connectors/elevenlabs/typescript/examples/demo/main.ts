// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { runWorker } from './worker.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
try { await runWorker(stop.signal); }
catch { console.error('ElevenLabs Worker could not start or stopped with an error.'); process.exitCode = 1; }
finally { process.off('SIGINT', handleSignal); process.off('SIGTERM', handleSignal); }
