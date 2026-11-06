# Jira Service Management Python connector

This package binds Jira Service Management to Dex Python. It supports customer lookup, ticket search and reads, request creation, label and priority updates, transitions, and comments. OAuth connection and refresh wiring is generated from the connector manifest.

The runnable example under `examples/support_request/` shows the static `ConnectionName` and Worker entrypoint. Atlassian authorization requires the Jira Service Management and Jira API scopes in the connector authorization guide.
