// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerLogLevel, TriggerLogger } from '../src/index.js';

/** One captured Trigger record. */
export interface RecordedLog { level: TriggerLogLevel; message: string; attributes: Record<string, unknown> }

/** Create a logger that keeps every record for assertions. */
export function newRecordingLogger(): TriggerLogger & { records: RecordedLog[] } {
  const records: RecordedLog[] = [];
  return { records, isEnabled: () => true, log: (level, message, attributes) => { records.push({ level, message, attributes: { ...attributes } }); } };
}
