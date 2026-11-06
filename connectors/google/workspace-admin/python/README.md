# Google Workspace Admin for Python

This package ports all seven Google Workspace Admin Directory operations to
Dex Python. Use the generated `new_project_connection` binding with the static
connection name used by your Flow. Both Workspace domain delegation and Google
OAuth follow the connector manifest. See the
[runnable account-list Flow](examples/account_lifecycle/README.md) and the
[connector setup guide](../README.md).

The package has been replayed against Go-recorded provider scenarios and run
through a real Dex Worker with a local Directory stand-in. Authorization and
Directory behavior against a live Workspace tenant remain unverified.
