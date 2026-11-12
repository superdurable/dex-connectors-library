# Slack Python connector

This package exposes Dex Query Steps for Slack channel pages, the connected workspace identity, thread pages, and replies, plus durable Mutation Steps for posting channel messages and thread replies. It uses the static `ConnectionName` configured in Dex Web. OAuth credentials are resolved at call time; the generated project connection refreshes rotating bot and user tokens through the manifest declaration.

The runnable example under `examples/thread_messages/` exercises the read and write operations against Dex. For real Slack use, create a Slack app in [Your Apps](https://api.slack.com/apps), configure OAuth and Socket Mode as described in the [connector guide](../README.md), then connect it in Dex Web. The Python package does not implement Socket Mode Triggers.

Every Query Step checks the HTTP status before decoding the body: HTTP 429 and 5xx return Retry and honor `Retry-After`, even when Slack sends no JSON body. From 0.4.1 this includes thread pages and replies.
