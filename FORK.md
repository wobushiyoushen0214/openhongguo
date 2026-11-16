# 本分支的额外修改

本分支基于 [beilunyang/openhongguo](https://github.com/beilunyang/openhongguo) 的上游 main，保留其 0.3.0 签名实现、模块路径和版本号。额外提供显式的批量播放入口，用于单集接口空响应时由应用选择备用路径；这是个人 fork 的修改，不是上游正式发行。

## 批量播放入口

Go 调用 `client.ResolvePlaybackMulti(ctx, episodeID, sdk.PlaybackOptions{})`；Android 调用 `client.resolvePlaybackMulti(episodeId)`，结果与原单集播放方法保持相同的播放模型、内容密钥和密钥错误结构。

每次请求只包含一个分集 ID，使用 `/novel/player/multi_video_model/v1/`。复用上游匿名档案及完整 Argus／Ladon 播放签名，不发送 Medusa；只解析响应中对应请求 ID 的桶，缺桶返回内容不可用，不能用别的分集替代。受保护模型沿用原内容密钥推导。空响应、认证拒绝、限流及取消保持现有分类与恢复选项。

此入口不会自行在单集和批量之间切换，调用方决定是否启用备用路径。momofish 的域名回退、成功路径记忆、前后台预算和失败冷却留在应用侧。此次公开 Go 和 Android 方法；共享绑定分发已支持新方法，Node.js／iOS／React Native／Flutter 的公开封装没有在本分支扩展。

## 验证与构建

```sh
go test ./bindings/internal/dispatch -run TestMultiPlaybackSelectsRequestedBucket -count=1 -v
go test ./internal/protocol/sign -count=1
export ANDROID_HOME=/你的/Android/sdk
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/28.2.13676358"
bash bindings/mobile/build.sh android
```

移动构建需要上游规定的 Go、gomobile／gobind、JDK 和 Android 工具链，生成 JNI 与 Kotlin AAR 并执行 Kotlin 契约测试。Go 测试覆盖对象／字符串模型、缺请求桶、空响应、认证拒绝和限流；Android 新用例核对批量方法编码及内容密钥成功／失败时的模型保留。

维护依据与验证范围见[决定记录](.agents/notes/implemented/feature/2026-11-16-multi-playback.md)。
