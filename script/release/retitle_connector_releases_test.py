# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

from __future__ import annotations

import sys
import textwrap
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import retitle_connector_releases as retitle  # noqa: E402


class RetitleConnectorReleasesTest(unittest.TestCase):
    def test_parses_legacy_root_tag_as_go(self) -> None:
        self.assertEqual(
            retitle.parse_connector_release_tag("connectors/google/gmail/v0.3.0"),
            retitle.ConnectorReleaseTag("connectors/google/gmail/v0.3.0", "connectors/google/gmail", "go", "v0.3.0"),
        )

    def test_parses_language_tags_of_nested_directories(self) -> None:
        for language in ("go", "python", "typescript"):
            tag = f"connectors/google/gmail/{language}/v0.4.0-rc.2"
            self.assertEqual(
                retitle.parse_connector_release_tag(tag),
                retitle.ConnectorReleaseTag(tag, "connectors/google/gmail", language, "v0.4.0-rc.2"),
            )

    def test_parses_single_segment_directory(self) -> None:
        self.assertEqual(retitle.parse_connector_release_tag("connectors/zoom/v0.24.0").directory, "connectors/zoom")
        self.assertEqual(retitle.parse_connector_release_tag("connectors/stripe/python/v0.1.0").language, "python")

    def test_ignores_sdk_and_malformed_tags(self) -> None:
        for tag in ("sdkgo/v0.5.0", "sdkpython/v0.1.0", "connectors/zoom/v0.24", "connectors/zoom/0.24.0",
                    "connectors/zoom/v0.24.0-rc.0", "connectors/zoom/v0.24.0-beta.1", "connectors/v0.1.0"):
            self.assertIsNone(retitle.parse_connector_release_tag(tag), tag)

    def test_reads_metadata_display_name_only(self) -> None:
        manifest = textwrap.dedent("""\
            apiVersion: connectors.dex.dev/v1alpha1
            spec:
              displayName: Wrong
            metadata:
              name: gmail
              displayName: "Gmail"
            """)
        self.assertEqual(retitle.read_manifest_display_name(manifest), "Gmail")
        with self.assertRaises(ValueError):
            retitle.read_manifest_display_name("metadata:\n  name: gmail\n")

    def test_plans_titles_and_skips_unknown_directories(self) -> None:
        releases = [
            {"tag_name": "connectors/zoom/v0.24.0", "name": "Zoom v0.24.0"},
            {"tag_name": "connectors/stripe/typescript/v0.1.0", "name": "Stripe typescript v0.1.0"},
            {"tag_name": "connectors/stripe/go/v0.25.0-rc.1", "name": "Stripe Go v0.25.0-rc.1"},
            {"tag_name": "connectors/removed/v0.1.0", "name": "Removed v0.1.0"},
            {"tag_name": "connectors/removed/python/v0.2.0", "name": "Removed python v0.2.0"},
            {"tag_name": "connectors/unknown/v0.1.0", "name": "Custom name"},
            {"tag_name": "sdkgo/v0.5.0", "name": "Go SDK v0.5.0"},
        ]
        display_names = {"connectors/zoom": "Zoom", "connectors/stripe": "Stripe", "connectors/removed": None,
                         "connectors/unknown": None}
        plans = {plan.tag: plan for plan in retitle.plan_release_retitles(releases, display_names)}
        self.assertEqual(set(plans), {release["tag_name"] for release in releases[:6]})
        self.assertEqual(plans["connectors/zoom/v0.24.0"].new_title, "Zoom Go v0.24.0")
        self.assertTrue(plans["connectors/zoom/v0.24.0"].is_change_required)
        self.assertEqual(plans["connectors/stripe/typescript/v0.1.0"].new_title, "Stripe TypeScript v0.1.0")
        self.assertFalse(plans["connectors/stripe/go/v0.25.0-rc.1"].is_change_required)
        self.assertEqual(plans["connectors/removed/v0.1.0"].new_title, "Removed Go v0.1.0")
        self.assertEqual(plans["connectors/removed/python/v0.2.0"].new_title, "Removed Python v0.2.0")
        self.assertFalse(plans["connectors/unknown/v0.1.0"].is_change_required)
        self.assertIn("no manifest", plans["connectors/unknown/v0.1.0"].skip_reason)


if __name__ == "__main__":
    unittest.main()
