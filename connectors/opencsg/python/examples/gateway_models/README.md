# opencsg listGatewayModels Flow

This runnable Flow uses the static `opencsg` connection and stores the read-only provider result in Dex. Run `uv run python -m examples.gateway_models.worker` after configuring the connection in Dex Web. Start `OpencsgListGatewayModels` with {}.

Generate strict FDG 2.0 without opening a browser:

```bash
dexcli visualize ./examples/gateway_models/flow.py --schema-version 2.0 --json --open=false --out /tmp/opencsg-python-fdg
```

The integration test exercises a real Dex Worker and Client with a fake provider. Live provider authorization and responses remain unverified.
