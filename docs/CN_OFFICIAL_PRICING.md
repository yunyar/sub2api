# 国产模型官方价格同步

默认在启动时读取本地缓存并尝试更新，之后每 6 小时从官方文档刷新；调度器按 `pricing.hash_check_interval_minutes` 检查是否到期。四个来源分别更新，其中一个失败不会阻止其他来源，也不会覆盖该来源上次成功的价格。

| 来源 | 官方文档 | 范围 |
| --- | --- | --- |
| Kimi | https://platform.kimi.ai/docs/pricing/chat | 文档明确列出的付费聊天模型，含缓存读取和缓存写入 |
| DeepSeek | https://api-docs.deepseek.com/quick_start/pricing | 文档明确列出的模型及官方确认的旧名称 |
| MiniMax | https://platform.minimax.io/docs/guides/pricing-paygo.md | LLM 标准、Priority 和可表达的长上下文阶梯 |
| Z.AI | https://docs.z.ai/guides/overview/pricing.md | 国际站文档明确列出的 GLM 聊天模型 |

只接受这些来源的美元价格，按每百万 token 转换成内部每 token 单价，不推测汇率。Z.AI 国际站价格不代表智谱国内站人民币价格。Qwen、豆包、混元等尚未接入直接官方文档同步，继续使用已有价格目录和运营者设置。不会按模型家族猜测新版本的价格。

## 配置

```yaml
pricing:
  cn_official_enabled: true
  cn_official_update_interval_hours: 6
```

缓存位于 `pricing.data_dir/cn_official_pricing.json`，采用临时文件加原子替换。重启后可继续使用验证通过的缓存。来源失败、表结构变化、缺失价格、非美元计价或无法准确表达的阶梯都会保留上次成功结果；没有历史结果则继续走已有目录与回退逻辑。这里没有修改所有未知模型的请求放行或零费用记录策略。

启用了 URL 白名单且自行固定过 `security.url_allowlist.pricing_hosts` 的安装，需要允许 `platform.kimi.ai`、`api-docs.deepseek.com`、`platform.minimax.io`、`docs.z.ai`。新默认配置包含这些主机。请求沿用现有定价下载代理，限定 HTTPS、同源重定向和 8 MiB 响应上限。

## 分组和计费

分组、渠道的显式价卡仍然优先；`pricing.override_file` 也继续覆盖默认价格。官方源优先于普通目录。分组倍率保持原值，实际扣费仍按现有计费流程应用倍率。

在分组创建或编辑页点击“恢复国产模型自动定价”，会先查询该模型是否已经获得官方价格，仅移除已验证模型的 token 自定义价卡。未知模型、通配符、图片、按次计费和验证失败的条目均保留。点击保存分组后才生效；不会自动修改生产分组。

Kimi K3.1 等精确模型名称新增离线回退价，防止首次更新失败时落入旧的宽泛 K2 回退。在线成功后，以新官方价格为准。

DeepSeek 的峰谷单价会验证峰价为谷价两倍，再沿用已有时段计算。现有时段实现识别工作日和周末，尚不识别中国法定节假日；节假日价格需要运营者通过显式价卡管理。MiniMax 的长上下文阶梯仍受现有长上下文计费开关控制。

## 状态和验证

现有定价状态接口中的 `cn_official` 包含每个来源的 URL、最近尝试和成功时间、错误、是否过期及模型数量。超过三个更新周期未成功即标记过期；过期不自动清空价格。管理员强制更新接口同时触发官方来源更新，失败会返回错误并保留有效缓存。

单元测试覆盖价格单位、币种、文档结构变化、缓存重启、失败保留、显式覆盖、实际 token/缓存计费、倍率和 HTTP 下载限制。可额外把四份官方原始文档保存为 `kimi.txt`、`deepseek.txt`、`minimax.txt`、`zai.txt`，设置 `SUB2API_CN_OFFICIAL_DOCUMENT_DIR` 后运行 `TestCNOfficialCapturedProviderDocuments`，验证当前真实文档。

## 官方多图计费更新的定制兼容

合并官方 `ba81c7fd758e425d67f856d601147ef0c5263d3f` 后保留实际返回图片张数解析、渠道按上下文分档的按次价格，以及 `248_group_image_count_billing.sql` 的兼容字段。该迁移与已有 `248_auto_routing_model_catalog_filter.sql` 按完整文件名分别记录，可从现有定制数据库升级并重复执行迁移检查。

本定制版继续遵循原有约定：有图片产出的生图请求始终按张结算，使用生图专属倍率，倍率为零也保持免费；不切回渠道 token 价格。管理页面显示固定的定制计费提示，不提供会误导管理员的关闭按张计费开关。对应的混合产图请求回归测试也按此定制约定验证，而不是采用官方默认的 token 优先策略。没有图片产出的普通聊天仍沿用 token 计费。

已有 Playground 数据库余额冻结继续优先于普通 Redis 预留，不重复预扣；结算和释放仍以同一终态去重键保证幂等。认证缓存版本提升到 26，避免两个分支各自的版本 25 缓存结构混用。发布镜像不自动激活生产服务。
