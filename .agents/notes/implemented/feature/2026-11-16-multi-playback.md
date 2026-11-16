# Agent Note: 为 Go 与 Android 增加显式批量播放备用入口

Status: implemented

## Problem

上游0.3.0已包含完整播放签名修复，但没有批量播放入口。momofish之前的同身份矩阵发现单集主／hl都空时，批量接口仍可能返回请求分集的模型与有效密钥；用户要求将额外修改推送到个人fork，以便独立维护。

## Decision

基于上游main的bf8df22，新增Go ResolvePlaybackMulti、共享绑定resolvePlaybackMulti分发和Android同名方法。每次只请求一个分集，使用已有匿名档案、完整Argus／Ladon签名及内容密钥推导；返回桶必须匹配请求ID，缺桶报内容不可用。受保护模型保留模型和内容密钥／密钥错误，其他错误和取消沿用上游边界。原单集方法与SDK默认恢复策略不改。

域名回退、前后台预算、成功路径记忆与失败冷却由应用负责。公开API范围为Go与Android，共享分发可供其他绑定后续接入；不改模块路径或冒充上游正式Maven版本。使用专用codex/android-multi-playback分支，保留fork已有main。

## Alternatives considered

- 删除批量并只用官方单集：丢失已有实际恢复路径，保留显式备用入口。
- SDK内部自动组合所有路径和设备恢复：会扩大行为及预算契约，本次由应用按场景控制。
- 应用复制签名和档案协议：维护面比SDK最小入口大，不采用。
- 将所有语言封装一次扩展：本次使用方是Android，保留Go与共享分发，不扩大未经验证的公开API。

## Validation

- Go批量分发1个方法／6个场景通过：对象模型、字符串模型、缺请求桶、空响应、认证拒绝和限流。
- 播放签名6个方法通过：SM3、Ladon、Argus向量、输入绑定与请求头形状。
- Android JNI从独立克隆生成，Kotlin／AAR构建成功，debug与release各9个ClientTest方法通过。新增批量用例覆盖请求方法／参数和密钥成功／失败时的模型保留，两构建变体不计作18个不同方法。
- 新增测试首次误用hex字段，编译失败；按实际JSON key／version和ContentKey.key契约修正后通过，失败不计通过。
- git diff --check与gofmt检查通过。公开接口机制及复跑见[本分支说明](../../../../FORK.md)。

## Consequences

已有批量备用能力成为可独立维护、可断言的fork提交，应用不必复制协议；代价是需跟踪上游协议和维护额外Go／Android API。当前不保证所有匿名设备、网络、剧目或分集可播。没有验证其他语言公开封装、实体手机或长期稳定性；新生成fork AAR未另外安装进App，应用端实播沿用此前同实现的组合证据，不计作本fork新包验收。
