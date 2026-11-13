# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

import http.client
import io
import json
import os
import ssl
import tempfile
import unittest
import urllib.error
from pathlib import Path
from unittest.mock import patch

from script.release import github_release_inventory as inventory


class Response(io.BytesIO):
    def __init__(self, body: object, links: str = "", headers: dict[str, str] | None = None) -> None:
        super().__init__(json.dumps(body).encode())
        self.headers = {"Link": links, **(headers or {})}


def graphql_release(tag: str, assets: list[str], *, has_more_assets: bool = False, **fields: object) -> dict[str, object]:
    return {"tagName": tag, "isDraft": False, "isPrerelease": False,
            "releaseAssets": {"pageInfo": {"hasNextPage": has_more_assets}, "nodes": [{"name": name} for name in assets]},
            **fields}


def graphql_page(releases: list[dict[str, object]], end_cursor: str | None) -> dict[str, object]:
    return {"data": {"repository": {"releases": {
        "pageInfo": {"hasNextPage": end_cursor is not None, "endCursor": end_cursor}, "nodes": releases}}}}


class GitHubReleaseInventoryTests(unittest.TestCase):
    def test_paginates_releases_and_keeps_asset_names(self) -> None:
        bodies = []

        def fetch(url: str, *, body: dict[str, object]):
            self.assertEqual(inventory.GRAPHQL_URL, url)
            bodies.append(body)
            if len(bodies) == 1:
                return graphql_page([graphql_release("sdkpython/v0.1.0", ["language-release.complete"]),
                                     graphql_release("sdkpython/v0.2.0", [], isPrerelease=True)], "cursor-1"), ""
            return graphql_page([graphql_release("connectors/acme/go/v0.1.0", [])], None), ""

        self.assertEqual({"sdkpython/v0.1.0": ["language-release.complete"],
                          "connectors/acme/go/v0.1.0": []}, inventory.release_inventory(fetch=fetch))
        self.assertEqual([None, "cursor-1"], [body["variables"]["after"] for body in bodies])
        self.assertEqual({"owner": "superdurable", "name": "dex-connectors-library", "after": None},
                         bodies[0]["variables"])

    def test_reads_more_than_one_thousand_releases(self) -> None:
        # The REST release list answers HTTP 422 past its first 1,000 results; cursors have no such cap.
        tags = [f"connectors/acme{index}/go/v0.1.0" for index in range(1_234)]
        cursors = []

        def fetch(_url: str, *, body: dict[str, object]):
            after = body["variables"]["after"]
            cursors.append(after)
            start = 0 if after is None else int(after)
            end = min(start + 100, len(tags))
            return graphql_page([graphql_release(tag, ["connector-release.complete"]) for tag in tags[start:end]],
                                str(end) if end < len(tags) else None), ""

        assets = inventory.release_inventory(fetch=fetch)
        self.assertEqual(set(tags), set(assets))
        self.assertEqual(13, len(cursors))
        self.assertEqual(["connector-release.complete"], assets[tags[-1]])

    def test_reads_a_release_with_more_than_one_asset_page_by_tag(self) -> None:
        tag = "connectorsmcp/v0.1.0"
        urls = []

        def fetch(url: str, *, body: dict[str, object] | None = None):
            urls.append(url)
            if body is not None:
                return graphql_page([graphql_release(tag, ["first.tgz"], has_more_assets=True)], None), ""
            return {"assets": [{"name": "second.tgz"}, {"name": "first.tgz"}]}, ""

        self.assertEqual({tag: ["first.tgz", "second.tgz"]}, inventory.release_inventory(fetch=fetch))
        self.assertTrue(urls[1].endswith("/releases/tags/connectorsmcp%2Fv0.1.0"))

    def test_graphql_errors_and_invalid_pages_fail_the_inventory(self) -> None:
        for response, message in (({"errors": [{"message": "API rate limit exceeded"}]}, "rate limit exceeded"),
                                  ({"data": {"repository": None}}, "invalid page"),
                                  (graphql_page([graphql_release("", [])], None), "invalid release"),
                                  ({"data": {"repository": {"releases": {"pageInfo": {"hasNextPage": True},
                                                                         "nodes": []}}}}, "no end cursor")):
            with self.subTest(message=message), self.assertRaisesRegex(RuntimeError, message):
                inventory.release_inventory(fetch=lambda _url, *, body, page=response: (page, ""))

    def test_records_release_creation_times_by_tag(self) -> None:
        def fetch(_url: str, *, body: dict[str, object]):
            return graphql_page([
                graphql_release("connectors/acme/python/v0.1.0", [], createdAt="2026-11-06T18:00:00Z"),
                {"tagName": "connectors/draft/python/v0.1.0", "isDraft": True, "createdAt": "2026-11-06T19:00:00Z"},
                graphql_release("connectors/undated/python/v0.1.0", [])], None), ""

        assets, created_at = inventory.release_inventory_with_creation_times(fetch=fetch)
        self.assertEqual({"connectors/acme/python/v0.1.0": [], "connectors/undated/python/v0.1.0": []}, assets)
        self.assertEqual({"connectors/acme/python/v0.1.0": "2026-11-06T18:00:00Z"}, created_at)

    def test_waits_out_graphql_rate_limits_reported_with_http_200(self) -> None:
        page = graphql_page([], None)
        for name, limited in (
                ("RATE_LIMITED error type", Response({"errors": [{"type": "RATE_LIMITED", "message": "API rate limit"}]},
                                                     headers={"X-RateLimit-Reset": "1030"})),
                ("exhausted remaining", Response({"errors": [{"message": "limit"}]},
                                                 headers={"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1030"})),
                ("secondary retry-after", Response({"errors": [{"message": "secondary rate limit"}]},
                                                   headers={"Retry-After": "45"}))):
            with self.subTest(name), patch.object(inventory.urllib.request, "urlopen",
                                                  side_effect=[limited, Response(page)]) as open_url:
                delays: list[float] = []
                result, _ = inventory.github_json(inventory.GRAPHQL_URL, body={"query": "{}"},
                                                  sleep=delays.append, clock=lambda: 1000)
                self.assertEqual(page, result)
                self.assertEqual([45.0 if "retry" in name else 32.0], delays)
                self.assertEqual(2, open_url.call_count)

    def test_graphql_rate_limit_beyond_the_budget_fails(self) -> None:
        limited = Response({"errors": [{"type": "RATE_LIMITED"}]}, headers={"X-RateLimit-Reset": "5000"})
        with patch.object(inventory.urllib.request, "urlopen", return_value=limited):
            with self.assertRaisesRegex(RuntimeError, "GraphQL.*exceeds"):
                inventory.github_json(inventory.GRAPHQL_URL, body={"query": "{}"}, sleep=lambda _: None,
                                      clock=lambda: 1000)

    def test_other_graphql_errors_fail_without_waiting(self) -> None:
        not_found = {"errors": [{"type": "NOT_FOUND", "message": "Could not resolve to a Repository"}]}
        with patch.object(inventory.urllib.request, "urlopen", return_value=Response(not_found)) as open_url:
            delays: list[float] = []

            def fetch(url: str, *, body: object | None = None) -> tuple[object, str]:
                return inventory.github_json(url, body=body, sleep=delays.append)

            with self.assertRaisesRegex(RuntimeError, "Could not resolve to a Repository"):
                inventory.release_inventory(fetch=fetch)
        self.assertEqual([], delays)
        self.assertEqual(1, open_url.call_count)

    def test_posts_graphql_bodies_as_json(self) -> None:
        with patch.object(inventory.urllib.request, "urlopen", return_value=Response({"data": {}})) as open_url:
            inventory.github_json(inventory.GRAPHQL_URL, body={"query": "{}"})
        request = open_url.call_args.args[0]
        self.assertEqual("POST", request.get_method())
        self.assertEqual(b'{"query": "{}"}', request.data)
        self.assertEqual("application/json", request.get_header("Content-type"))

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

    def test_retries_dropped_connections_then_reads_release(self) -> None:
        dropped = [http.client.RemoteDisconnected("Remote end closed connection without response"),
                   urllib.error.URLError("temporary failure in name resolution"), TimeoutError("read timed out")]
        with patch.object(inventory.urllib.request, "urlopen", side_effect=[*dropped, Response({"assets": []})]) as open_url:
            delays = []
            result, _ = inventory.github_json("https://api.github.com/test", sleep=delays.append, clock=lambda: 1000)
        self.assertEqual({"assets": []}, result)
        self.assertEqual([2, 4, 8], delays)
        self.assertEqual(4, open_url.call_count)

    def test_persistent_dropped_connection_raises_the_original_error(self) -> None:
        dropped = http.client.RemoteDisconnected("Remote end closed connection without response")
        with patch.object(inventory.urllib.request, "urlopen", side_effect=dropped) as open_url:
            with self.assertRaisesRegex(http.client.RemoteDisconnected, "Remote end closed"):
                inventory.github_json("https://api.github.com/test", sleep=lambda _: None, clock=lambda: 1000)
        self.assertEqual(5, open_url.call_count)

    def test_certificate_verification_failure_is_not_retried(self) -> None:
        rejected = urllib.error.URLError(ssl.SSLCertVerificationError(1, "certificate verify failed"))
        with patch.object(inventory.urllib.request, "urlopen", side_effect=rejected) as open_url:
            with self.assertRaises(urllib.error.URLError):
                inventory.github_json("https://api.github.com/test", sleep=lambda _: self.fail("retried"), clock=lambda: 1000)
        open_url.assert_called_once()

    def test_missing_release_is_not_retried(self) -> None:
        missing = urllib.error.HTTPError("https://api.github.com/test", 404, "Not Found", {}, io.BytesIO(b"{}"))
        with patch.object(inventory.urllib.request, "urlopen", side_effect=missing) as open_url:
            with self.assertRaises(inventory.GitHubNotFoundError):
                inventory.github_json("https://api.github.com/test", sleep=lambda _: None, clock=lambda: 1000)
        open_url.assert_called_once()


if __name__ == "__main__":
    unittest.main()
