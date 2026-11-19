# Huo15 model list Flow

This runnable Python Flow lists available Juxingyi model IDs through the static `huo15` connection and persists only the bounded IDs and count. Configure an API key in Dex Web Connections using the connector authorization guide, then run `python -m examples.model_list.worker` from this package directory with `DEX_FLOW_SERVICE_ADDRESS` set to the Dex Flow service.

The integration test uses a fake HTTP provider and real Dex Worker/Client. It verifies a 503 retry and persisted completion. Run strict FDG 2.0 generation with the package README command. Live authorization remains unverified.
