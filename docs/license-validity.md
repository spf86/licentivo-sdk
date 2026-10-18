# License validity / 许可证有效期

Set validity days when issuing each timed or trial license, not in its authorization policy. Supply `duration_days` from 1 to 36500; perpetual licenses omit it. Counting starts at the first successful activation. Applying a policy preserves the license term, expiry and renewals. Existing SDK 0.10.0 clients enforce the signed expiry without code or configuration changes.

有效天数在单张或批量签发许可证时填写，不在授权策略中设置。固定期限和试用许可证填写 `duration_days`（1–36500 天），永久授权不填。从首次成功激活开始计时；应用策略保留原期限、到期日和续期权益。现有 SDK 0.10.0 按签名中的到期时间校验，无需更改代码或配置。

Create one policy for reusable device, feature and runtime rules:

```json
{"product_id":"PRODUCT_UUID","name":"Standard","mode":"timed","max_devices":1,"features":["export"]}
```

Issue each license with its own duration through `POST /api/v1/licenses`:

```json
{"product_id":"PRODUCT_UUID","policy_id":"POLICY_UUID","customer":"Customer A","duration_days":30}
```

The same policy can issue a 365-day license by changing only `duration_days`. For batch creation, set a common `duration_days` and optionally override it in each entry. CSV imports accept `customer,customer_email,duration_days`; blank per-customer days use the common duration. Existing licenses retain their stored dates. Extend validity with renewal.

Legacy policy `duration_days` inputs are ignored. Management integrations must supply the term when creating timed/trial licenses. Runtime SDK activation, signed leases, offline verification, transfers and renewal handling keep the same protocol. Package versions and previously published release assets remain unchanged.

[English documentation](https://www.licentivo.com/en/docs.html#management) · [中文文档](https://www.licentivo.com/zh/docs.html#management)
