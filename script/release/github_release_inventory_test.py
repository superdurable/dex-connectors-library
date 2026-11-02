# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

import io
import json
import os
import tempfile
import unittest
import urllib.error
from pathlib import Path
from unittest.mock import patch

from script.release import github_release_inventory as inventory


class Response(io.BytesIO):
    def __init__(self, body: object, links: str = "") -> None:
        super().__init__(json.dumps(body).encode())
        self.headers = {"Link": links}


class GitHubReleaseInventoryTests(unittest.TestCase):
    def test_paginates_releases_and_keeps_asset_names(self) -> None:
        calls = []

        def fetch(url: str):
            calls.append(url)
            if len(calls) == 1:
                return ([{"tag_name": "sdkpython/v0.1.0", "assets": [{"name": "language-release.complete"}]},
                         {"tag_name": "sdkpython/v0.2.0", "prerelease": True, "assets": []}],
                        '<https://api.github.com/next>; rel="next"')
            return ([{"tag_name": "connectors/acme/go/v0.1.0", "assets": []}], "")

        self.assertEqual({"sdkpython/v0.1.0": ["language-release.complete"],
                          "connectors/acme/go/v0.1.0": []}, inventory.release_inventory(fetch=fetch))
        self.assertEqual("https://api.github.com/next", calls[1])

    def test_waits_for_reported_reset_then_reads_release(self) -> None:
        limited = urllib.error.HTTPError("https://api.github.com/test", 403, "Forbidden",
                                        {"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1009"},
                                        io.BytesIO(b'{"message":"API rate limit exceeded"}'))
        with patch.object(inventory.urllib.request, "urlopen", side_effect=[limited, Response({"assets": []})]) as open_url:
            delays = []
            result, _ = inventory.github_json("https://api.github.com/test", sleep=delays.append, clock=lambda: 1000)
        self.assertEqual({"assets": []}, result)
        self.assertEqual([11.0], delays)
        self.assertEqual(2, open_url.call_count)

    def test_rate_limit_error_is_not_mistaken_for_missing_release(self) -> None:
        limited = urllib.error.HTTPError("https://api.github.com/test", 403, "Forbidden",
                                        {"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "2500"},
                                        io.BytesIO(b'{"message":"API rate limit exceeded"}'))
        with patch.object(inventory.urllib.request, "urlopen", side_effect=limited):
            with self.assertRaisesRegex(RuntimeError, "rate limit.*exceeds"):
                inventory.github_json("https://api.github.com/test", sleep=lambda _: None, clock=lambda: 1000)

    def test_cached_assets_distinguish_missing_release(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "releases.json"
            path.write_text('{"connectors/acme/go/v0.1.0":["connector-release.complete"]}')
            self.assertEqual(frozenset({"connector-release.complete"}),
                             inventory.release_assets("connectors/acme/go/v0.1.0", str(path)))
            self.assertIsNone(inventory.release_assets("connectors/acme/go/v0.2.0", str(path)))

    def test_unavailable_inventory_falls_back_to_exact_release_lookup(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "releases.json"
            path.write_text('{"_inventory_unavailable":true}')
            calls = []

            def fetch(url):
                calls.append(url)
                return {"assets": [{"name": "connector-release.complete"}]}, ""

            self.assertEqual(frozenset({"connector-release.complete"}),
                             inventory.release_assets("connectors/acme/go/v0.1.0", str(path), fetch=fetch))
            self.assertEqual(1, len(calls))

    def test_live_tag_cli_ignores_stale_inventory(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "releases.json"
            path.write_text('{"connectors/acme/go/v0.1.0":[]}')
            with patch.dict(os.environ, {"GH_RELEASE_INVENTORY_PATH": str(path)}), \
                    patch.object(inventory, "release_assets", return_value=frozenset({"connector-release.complete"})) as lookup, \
                    patch("sys.argv", ["inventory", "--tag", "connectors/acme/go/v0.1.0", "--live"]), \
                    patch("sys.stdout", new_callable=io.StringIO) as output:
                inventory.main()
            lookup.assert_called_once_with("connectors/acme/go/v0.1.0", None)
            self.assertEqual('["connector-release.complete"]', output.getvalue().strip())

    def test_direct_lookup_treats_only_http_404_as_missing(self) -> None:
        missing = urllib.error.HTTPError("https://api.github.com/test", 404, "Not Found", {}, io.BytesIO(b"{}"))
        with patch.object(inventory.urllib.request, "urlopen", side_effect=missing):
            self.assertIsNone(inventory.release_assets("connectors/acme/go/v0.2.0"))


if __name__ == "__main__":
    unittest.main()
