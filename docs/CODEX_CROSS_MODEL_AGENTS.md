# Codex 跨模型子代理兼容

同一个 Auto Key 可以分别为父代理和子代理选择模型。每次请求仍独立检查模型、端点、候选组权限和余额，使用实际执行组的账号调度及计费；父代理的平台和实际组不作为子代理的路由条件。固定分组 Key 仍受该组原有模型范围限制。

## 协作与请求结束

单条模型请求的 `response.completed` 表示该次推理完成；包含 `function_call` 的完成事件仍可能需要客户端执行工具、等待子代理，再发起下一次推理。它不表示全部子代理工作完成。

Sub2API 转换并转发工具声明、调用和结果，不执行客户端的 `spawn_agent`、`wait_agent` 或 `close_agent`。本轮交付依赖子代理时，应由客户端和主代理等待、回收并汇总结果后再给最终答复；明确安排的后台任务另行管理。父代理给出最终答复不会让网关自动取消其他独立请求。

网关的流结束与 Auto 执行状态分别处理。上游已明确结束时应及时关闭本次流；没有完成证据的截断流应返回失败，保留已产生的用量，不能补成成功。Auto 的实际计费和请求终态可以先后发生，计费先完成也必须允许当前尝试收尾；这不允许重新派发或重复结算。

Responses 透传流使用 `gateway.stream_data_interval_timeout` 限制上游空闲等待；未收到响应终态时，工具参数结束不算整条响应完成。检测到客户端写入失败后，用量排水最多再持续该时长，超时保留已经读取的用量；配置为零或负数时保持禁用超时的原有语义。

Responses 原始数据中的可选 `phase` 字段在类型化 JSON 往返时保留。Anthropic 没有对应字段，跨协议转换不承诺无损保留 `commentary` / `final_answer` 标签，也不会根据文字内容猜测这些标签。

## 任务消息转换

Codex 的 `agent_message` 与普通 `reasoning` 不同。部分自定义 provider 请求把任务正文以明文放在 `agent_message.content` 内的 `encrypted_content` 字段。直接发送给原生 Responses 上游可能触发 `amsg_*` 加密内容校验错误。

兼容层只针对明确的 `agent_message`：

- 普通文本、任务信封和可转换的附件保持原顺序。
- 带 `Message Type:` 和 `Payload:` 信封、且未检测到密文特征的任务正文转换为 `input_text`。
- 原生 Responses 出站将可转换任务包装为普通 `user` 消息，不发送原有 `amsg_*` ID。
- Anthropic 和 Chat Completions 转换链使用相同内容规则，再转换为各自的用户消息格式。
- 模型、工具调用及其 ID、普通消息、普通 reasoning 项和计费字段不由这层改写；保留各协议已有的兼容处理。
- 未出现任务消息的请求不因这层转换重新序列化；原始请求字节不被原地修改。
- 内容审查和旧版 moderation 使用相同任务内容规则提取明文，按用户任务检查；管理员审查记录保留任务正文。不会将普通 reasoning 密文加入审查正文。

入口覆盖 OpenAI 原生 Responses（含 API Key、OAuth、透传和 WebSocket）、Grok Responses、Responses 经 Chat Completions 降级、原生 Anthropic 和 Antigravity Responses。请求是否能执行，仍取决于所选账号实际支持的协议和模型。

## 加密内容与附件边界

本功能不解密供应商密文。Fernet、Anthropic 签名内容及可识别的长 Base64 密文不能作为任务正文发送。缺少任务信封的 `encrypted_content` 也不推断为明文。检测属于兼容性判断，不是通用的密码学格式验证。

原生 Responses 路径保留不能安全转换的任务原项，由原供应商验证。跨协议转换无法表达这些内容时明确返回请求错误，不把密文当文本，也不丢掉正文后发起空任务。若客户端发送真正的跨供应商密文，需要客户端提供可移植的任务明文或摘要；单靠网关无法恢复原文。

图片与文件只在目标协议可表达时转换。例如，Anthropic 任务附件目前要求支持的内联 data URI；仅有供应商 `file_id` 的文件和不可转换的图片会报错。不会为兼容任务消息而擅自下载远程附件。

## 验证

定向测试使用本地模拟上游，检查最终出站请求及返回结果：

```sh
cd backend
go test -tags unit ./internal/pkg/apicompat ./internal/service \
  -run 'AgentMessage|AutoRoutingPrepareRequestKey(ConcurrentModelsKeepGroupAndBillingRequestLocal|FailureDoesNotPoisonNextPlatform)'
go test -tags unit ./internal/server/middleware \
  -run 'BindEffectiveGroupContextConcurrentRequestsKeepRoutingStateIsolated'
```

测试包括任务正文、附件顺序、密文边界、HTTP/WebSocket 入口以及不同模型请求的路由隔离。模拟上游通过不代表每个真实供应商、客户端版本和账号都已完成在线验收。

## Dragoman 参考核对

核对 `asakin/dragoman` 固定提交 `600d11690f8976c441f70849ea2f6d032cc30b94` 的 `dragoman/cli.py:cmd_ask` 和 `dragoman/routers/providers.py`。该实现读取独立任务消息，按所选模型分发到 OpenAI-compatible、Anthropic 或 Gemini 适配器，并返回文本和用量。它没有实现 Responses `agent_message` 或供应商密文的跨平台解密。因此这里只借鉴独立明文任务与供应商适配的边界，继续使用 Sub2API 原有的鉴权、路由、审查与计费。
