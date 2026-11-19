// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import eslint from '@eslint/js';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  eslint.configs.recommended,
  ...tseslint.configs.recommended,
  { files: ['src/**/*.ts', 'test/**/*.ts', 'examples/**/*.ts'], rules: { '@typescript-eslint/no-explicit-any': 'error' } },
);
