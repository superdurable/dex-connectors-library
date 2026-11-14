# Moxin (Kokoni)

The Go connector lists model IDs available to a Moxin platform token and its assigned group. `ListModels` has no input and returns only model IDs, not chat content or customer data. The [official platform guide](https://api.kokoniworld.com/docs-local.html) documents `GET https://api.kokoniworld.com/v1/models`, Bearer authentication, and `{object:"list",data:[{id,object:"model"}]}` responses. It advises calling this endpoint before choosing a model because availability depends on token and group permissions.

Moxin (魔芯（湖州）科技有限公司) opens enterprise accounts through an administrator. Sign in to the [platform console](https://api.kokoniworld.com/), open **令牌管理** (Token Management), and choose **新建令牌** (New Token). Set allowed models and a quota limit, then enter the new `sk-...` token in Dex Web's secret `platform_token` field without `Bearer`. The provider's documented public API base URL is HTTPS. The guide also contains an older general HTTP warning; this connector's default uses its explicitly documented HTTPS endpoint.

The [runnable Dex Flow](go/examples/model-list/README.md) shows connection setup and output. The logo embeds the unmodified [official website mark](https://api.kokoniworld.com/kokoni-platform-mark-20260814.png). No live account authorization has been verified.
