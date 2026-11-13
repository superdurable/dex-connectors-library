# Tushare Trading Calendar

This connector checks one Chinese securities exchange date through the [Tushare Pro `trade_cal` API](https://tushare.pro/document/2?doc_id=26). The [official privacy policy](https://tushare.pro/document/1?doc_id=410) identifies 北京沃远数据科技有限公司 as the platform operator.

Register at [Tushare Pro](https://tushare.pro), open 个人中心 > 接口 TOKEN, copy your secret token, and add a `tushare` Dex Web connection. Check `trade_cal` access and points in 权限中心. Supply `exchange` as `SSE`, `SZSE`, or `BSE`, and `date` as `YYYYMMDD`. The connector sends a fixed `trade_cal` POST over HTTPS and returns only the exchange, date, open flag, and previous trading date. It excludes the token and raw provider response from Flow output and errors. Blank timeout and maxResponseBytes use their displayed defaults.

The [runnable Dex example](go/examples/get-trading-day/) uses a synthetic mock by default. It does not verify live authorization or financial data freshness.
