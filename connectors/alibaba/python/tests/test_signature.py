# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Alibaba Cloud's published ACS3-HMAC-SHA256 vector."""

from dex_connectors.alibaba.client import _sign_acs3_request


def test_official_acs3_signature_vector() -> None:
    """Use the documented RunInstances vector to catch canonicalization drift."""
    signature = _sign_acs3_request(
        "YourAccessKeySecret",
        "POST",
        "ecs.cn-shanghai.aliyuncs.com",
        "RunInstances",
        "ImageId=win2019_1809_x64_dtc_zh-cn_40G_alibase_20230811.vhd&RegionId=cn-shanghai",
        "2023-10-26T10:22:32Z",
        "3156853299f313e23d1673dc12e1703d",
    )
    assert signature == "06563a9e1b43f5dfe96b81484da74bceab24a1d853912eee15083a6f0f3283c0"
