// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import { ListMembersInputSchema as GeneratedListMembersInputSchema } from './generated/models.js';

/** Accept Go time.Parse's RFC 3339 forms; the operation validates the timestamp. */
export const ListMembersInputSchema = GeneratedListMembersInputSchema.safeExtend({
  changedSince: z.string().nullable().optional(),
  optedInSince: z.string().nullable().optional(),
});
