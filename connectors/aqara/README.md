# Aqara device list

This Go connector reads one page of devices in an authorized Aqara account or project through the mainland China Cloud API. It returns device IDs, names, model, gateway relationship, position ID, firmware, timezone, and online state. The query is read-only and does not expose signing credentials or access tokens in Flow state.

The provider contract is documented in Aqara's [API call guide](https://opendoc.aqara.com/docs/%E4%BA%91%E5%BC%80%E5%8F%91/API%E4%BD%BF%E7%94%A8%E6%8C%87%E5%8D%97/API%E9%80%9A%E7%94%A8%E8%AF%B4%E6%98%8E.html), [device management API](https://opendoc.aqara.com/docs/%E4%BA%91%E5%BC%80%E5%8F%91/API%E6%96%87%E6%A1%A3/%E8%AE%BE%E5%A4%87%E7%AE%A1%E7%90%86%E6%8E%A5%E5%8F%A3.html), and [signing rules](https://opendoc.aqara.com/en/docs/developmanual/apiIntroduction/signGenerationRules.html). This connector uses v3 `query.device.info`, not the separately documented beta v4 REST API. Page size is limited to 100. `PageNum` and `PageSize` zero values select page 1 and 50 items.

Create an approved project at the [Aqara Developer Platform](https://developer.aqara.com), obtain its AppID and KeyID/AppKey from Console > Project Management > Project Details > Key Management, then authorize the Aqara Home account or industry project. Aqara documents [account authorization](https://opendoc.aqara.com/en/docs/developmanual/authManagement/aqaraauthMode.html) and [project authorization](https://opendoc.aqara.com/en/docs/developmanual/authManagement/projectauthMode.html). Put the resulting access token in the protected Dex credential field; update it manually before expiry. The default token lifetime is seven days, and the provider documents a refresh API. The connector does not exchange or refresh tokens automatically.

See the [runnable Dex Flow](go/examples/device-list/README.md). The mock integration tests run against a real Dex Worker and Client; live Aqara authorization, token renewal, rate limits, and device response have not been exercised.

The [TypeScript package and runnable Flow](typescript/README.md) replay the Go-recorded provider contract and exercise the Flow against a real local Dex Server.

The directory logo is the unmodified [official Aqara SVG](https://www.aqara.com/wp-content/uploads/2023/03/aqara-logo-dark.svg) displayed on Aqara’s [brand story](https://www.aqara.com/en/about-us/brand-story/) page.
