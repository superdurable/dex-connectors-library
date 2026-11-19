# pingpp getChargeStatus Flow

This runnable Flow uses the static `pingpp` connection and stores the read-only provider result in Dex. Run `uv run python -m examples.charge_status.worker` after configuring the connection in Dex Web. Start `PingppGetChargeStatus` with {"chargeId":"ch_sample123"}.

Generate strict FDG 2.0 without opening a browser:

```bash
dexcli visualize ./examples/charge_status/flow.py --schema-version 2.0 --json --open=false --out /tmp/pingpp-python-fdg
```

The integration test exercises a real Dex Worker and Client with a fake provider. Live provider authorization and responses remain unverified.
