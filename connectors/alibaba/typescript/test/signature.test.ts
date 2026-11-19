// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { signACS3Request } from '../src/signature.js';

it('matches the official Alibaba Cloud ACS3 signature vector', () => {
  // https://help.aliyun.com/en/sdk/product-overview/v3-request-structure-and-signature
  expect(signACS3Request(
    'YourAccessKeySecret', 'POST', 'ecs.cn-shanghai.aliyuncs.com', 'RunInstances',
    'ImageId=win2019_1809_x64_dtc_zh-cn_40G_alibase_20230811.vhd&RegionId=cn-shanghai',
    '2023-10-26T10:22:32Z', '3156853299f313e23d1673dc12e1703d',
  )).toBe('06563a9e1b43f5dfe96b81484da74bceab24a1d853912eee15083a6f0f3283c0');
});
