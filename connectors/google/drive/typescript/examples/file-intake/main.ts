// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newConsoleTriggerLogger } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { runFileIntake } from './app.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
try {
  const project = await loadFromEnvironment();
  await runFileIntake(project, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801', logger: newConsoleTriggerLogger('info') }, stop.signal);
} catch (error) {
  console.error(`Google Drive file intake stopped: ${error instanceof Error ? error.message : 'unknown error'}`);
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
