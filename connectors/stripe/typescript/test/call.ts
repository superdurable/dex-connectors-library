// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Call } from '@superdurable/connectors-sdk';

/** Empty Stream capabilities for provider tests that do not configure progress. */
export const noProgress: Pick<Call, 'reportProgress' | 'writeText' | 'hasProgressStream' | 'hasTextStream'> = {
  reportProgress: () => {},
  writeText: () => {},
  hasProgressStream: () => false,
  hasTextStream: () => false,
};
