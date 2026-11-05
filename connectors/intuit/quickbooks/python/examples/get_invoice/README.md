# QuickBooks invoice lookup

This Flow reads one invoice through the `getInvoice` Query and returns it on the `found` branch. The `notFound`, `providerRejected`, `invalidResponse`, and `defect` branches remain unwired, so Dex fails the Flow if any occurs.

Configure a QuickBooks Online OAuth connection named `quickbooks-invoice-reader` in Dex Web. Use the Intuit authorization guide in the [connector README](../../../README.md); select the matching production or sandbox environment. Run `python -m examples.get_invoice.main` with `DEX_FLOW_SERVICE_ADDRESS` pointing to the Dex gRPC address, then start `QuickBooksGetInvoicePython` with `{"invoiceId":"130"}` through a Dex Client. The invoice ID is a QuickBooks decimal string.

The integration test exercises this Flow against a real `dexcli dev` stack and a local HTTP stand-in; it does not contact Intuit. Start `dexcli dev --dex-port 42901 --web-port 42902 --open=false` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:42901 uv run pytest -q examples/get_invoice/integration_test.py` from `python/`. The test stand-in binds an OS-assigned free port.

## Flow Definition Graph

From `connectors/intuit/quickbooks/python`, generate strict FDG 2.0 for the checked-in Flow:

```sh
dexcli visualize examples/get_invoice/flow.py --language python \
  --schema-version 2.0 --json --out /tmp/quickbooks-python-get-invoice \
  --python .venv/bin/python
```

The generated JSON must report `valid: true` and have no diagnostics. The example's connector Step has exactly one explanation directive beside its group directive.
