# bili-cli

一个用于浏览, 管理和下载 Bilibili 内容的 Go 命令行工具. 覆盖账户, 视频, 搜索, 发现, 收藏, 动态, 互动和媒体下载工作流.

## 功能

- 视频详情, AI 总结, 评论和相关推荐.
- 字幕轨道列出与导出, 覆盖普通字幕和平台标记的 AI 字幕.
- UP 主资料, 视频列表, 合集和系列列表, 专栏, 视频, 用户, 番剧, 直播和影视搜索.
- 热门视频, 排行榜, 收藏夹, 关注, 历史和稍后再看.
- 动态时间线, 我的动态, 文字动态发布和删除.
- 点赞, 投币, 一键三连和取消关注.
- 保存凭证, QR 登录, JSON/YAML 稳定 envelope 输出.
- 视频音频流和视频流下载, 支持仅下载音频或视频.
- 直播流下载, 支持 FLV 和 HLS, 可指定画质并持续录制.
- 本地轮询订阅: UP 主新视频 / 开播, 视频数据阈值, 动态, 关键词搜索和合集更新.

## 致谢

本项目的早期功能迁移和命令设计参考了上游 Python 项目 [public-clis/bilibili-cli](https://github.com/public-clis/bilibili-cli), App 请求路径参考了 [bggRGjQaUbCoE/PiliPlus](https://github.com/bggRGjQaUbCoE/PiliPlus). 感谢两个项目提供的实现和思路.

## 构建

```shell
just build
./bin/bili --help
./bin/bili --version
```

`bili --version` 在版本 tag 上直接显示 tag. 后续干净提交显示最近 tag 和短 commit, 脏工作区使用 `^` 分隔. 首个版本 tag 之前使用 `devel` 作为基础版本.

也可以直接运行:

```shell
go run ./cmd/bili video hot --max 5 --yaml
```

## 登录

```shell
go run ./cmd/bili me login
go run ./cmd/bili me --yaml
```

扫码登录会同时保存 Web Cookie, App access token 和 refresh token. 升级到此版本后, 请重新执行一次 `bili me login` 以启用 App 请求路径.

手机号短信登录:

```shell
go run ./cmd/bili me login --phone 13800138000
```

命令会发送短信并交互读取验证码. 也可以使用 `--code` 传入验证码, 使用 `--country-code` 指定国家区号, 默认值为 `86`. 如果 Bilibili 要求人机验证, 命令会输出本地验证地址. 在浏览器完成验证后, 登录会自动继续.

## 字幕

```shell
go run ./cmd/bili video subtitle BV1ABcsztEcY
go run ./cmd/bili video subtitle BV1ABcsztEcY --language zh_CN --type non-ai
go run ./cmd/bili video st BV1ABcsztEcY -o ./subtitles
go run ./cmd/bili video st "https://www.bilibili.com/video/BV1ABcsztEcY?p=2" -o ./downloads
```

`bili video subtitle` 会列出播放器接口返回的所有字幕轨道, 包含 ID, 语言, 字幕类型, AI 标记, 作者和字幕行数. 可通过 `--id`, `--language` 或 `--type all|ai|non-ai` 筛选轨道. `--language` 使用 API 的 `lan`, 并兼容 `zh-CN` 与 `zh_CN` 两种写法. `-o` 始终指定输出目录, 字幕会写为 `<视频基名>.<语言>.srt`, 与下载视频共享基名以便播放器自动识别. 多分P视频需通过 URL 的 `?p=N` 选择对应字幕.

## 视频下载

```shell
bili video download BV1ABcsztEcY
bili video download BV1ABcsztEcY -o ./downloads
bili video download "https://www.bilibili.com/video/BV1ABcsztEcY?p=2"
bili video download BV1ABcsztEcY --audio-only
bili video download BV1ABcsztEcY --video-only
bili video download BV1ABcsztEcY --no-merge
bili video download BV1ABcsztEcY --with-srt
```

`bili video download` 默认保存到当前文件夹. DASH 地址会分别保存 `<标题>_audio.m4a` 和 `<标题>_video.m4a`, 然后尝试使用 `ffmpeg -c copy` 生成 `<标题>.mp4`. `--no-merge` 可保留两个流而不合并. 原生 `durl` 地址会直接保存一个 `.mp4`.

媒体下载会在服务器支持 HTTP Range 时使用配置的并发线程数分段下载, 不支持时自动回退为单请求下载.

多分P视频必须在 URL 中指定 `?p=N`. 未指定时会列出所有分P及其标题, 不会下载第一P. 文件名会包含 `P` 序号和该分P的标题. `--with-srt` 会尝试选择并保存一条字幕, 优先中文, 同语言优先人工字幕, 其次 AI, 再按英文和其他语言兜底. 交互终端会显示下载进度条. `--audio-only` 和 `--video-only` 不能同时使用.

## 直播下载

```shell
bili live download 5440
bili live download "https://live.bilibili.com/5440" -o ./live
bili live download 5440 --quality 400 --format flv
bili live download 5440 --quality 原画
bili live download 5440 --quality 蓝光
bili live download 5440 --max-duration 30m
bili live download 5440 --max-size 2GB
bili live download 5440 --max-duration 2h --max-size 8GB
bili live download 5440 --format hls
```

`bili live download` 会获取当前直播间的播放地址并持续写入文件, 直到直播结束或按下 `Ctrl-C`. 文件名使用直播标题和开始录制时间 `<直播标题>_YYYYMMDD_HHMMSS.flv`, 不再仅使用房间号. 默认保存为 `.flv`, `--format hls` 保存为 `.ts`. `--quality` 支持 `流畅`, `高清`, `蓝光`, `原画`, `4K`, `杜比` 和数字画质编号, 默认是 `原画`. `--max-duration` 使用 `30m`, `2h` 这样的时长格式, `--max-size` 使用 `512MB`, `2GB` 这样的大小格式. 设置限制后, 交互终端会在一行进度条中显示当前值和上限, 达到任一上限后自动停止并保留录制文件. 未设置的限制不显示对应进度. 按下 `Ctrl-C` 会保留已录制内容并完成临时文件清理. 录制结束后会尝试使用 `ffmpeg -c copy` 修复文件时长元数据, `ffmpeg` 不存在或修复失败时只输出警告并保留原始文件. 直播下载是读取操作, 在 `safety.read_only = true` 下可用. 直播流通常没有总长度, 交互终端会显示已写入字节数和已录制时长, 日志会按阶段输出进度.

## 用户列表

```shell
bili user lists 7855491
bili user lists "作者名称"
bili user lists 7855491/8565435
bili user lists "作者名称/8565435"
bili user lists "https://space.bilibili.com/7855491/lists?sid=8565435"
bili user lists https://b23.tv/example
```

`bili user` 和 `bili user lists` 都接受 UID, 用户名, Bilibili 空间链接或 b23 短链. `bili user lists UID_OR_NAME` 显示该 UP 主的全部列表, 用户名解析行为与 `bili user` 一致. 传入 `UID_OR_NAME/SID` 或 Bilibili 空间列表链接时, 会显示指定列表中的视频. `--page` 在用户查询时翻阅列表目录, 在指定列表查询时翻阅视频. 命令会自动识别 Bilibili 列表类型并使用统一输出.

## 图片下载

```shell
bili image BV1ABcsztEcY
bili image https://www.bilibili.com/read/cv42 -o ./images
bili image https://b23.tv/example
bili image user 946974
bili image live 5440 --with-avatar
```

`bili image REF` 会自动识别视频 BV 号, 专栏 cv 号, 番剧 ss 或 ep 号, 影视 md 号, 标准 Bilibili 页面 URL 和 b23 短链. 无法识别的裸参数默认按用户 UID 或用户名处理. 可通过 `bili image user|up|video|article|bangumi|media|live REF` 明确指定类型.

用户下载头像, 其他对象默认下载封面. `--with-avatar` 会额外下载作者或主播头像. 资源保存为稳定 ID 文件名, 例如 `video-BV1ABcsztEcY-cover.jpg`. 同名文件会直接覆盖. 主图下载成功后, 作者头像不可用只会报告警告而不会删除主图. 可使用 `--json` 或 `--yaml` 输出下载路径, 字节数和警告. 图片下载仅使用读取请求, 在 `safety.read_only = true` 下可用.

其他命令按领域组织在 `bili me`, `bili user`, `bili video` 和 `bili dynamic` 下. 例如 `bili user UID_OR_NAME_OR_URL`, `bili user video UID_OR_NAME_OR_URL`, `bili user lists UID_OR_NAME`, `bili user follow UID`, `bili me fav`, `bili video watch` 和 `bili dynamic post TEXT`. `bili video watch` 仍是稍后再看, 本地轮询订阅使用根命令 `bili watch`.

配置和认证默认保存在 `~/.config/bilibili-cli/config.toml` 与 `~/.config/bilibili-cli/auth.json`. 订阅规则保存在同目录的 `watch.json`. 如需从已有 cookie 导入, 可以传入 `BILI_COOKIE` 或 Netscape cookie 文件路径 `BILI_COOKIE_FILE`.

## 配置

使用 `bili config init` 创建默认 `config.toml`. 根目录的 [`config.toml.example`](./config.toml.example) 可作为手动创建配置的模板.

```shell
bili config status
bili config status --json
bili config upgrade
```

`bili config status` 显示配置文件和每个已知字段的 `missing`, `set` 或 `error` 状态, 并列出解析错误. `bili config upgrade` 会将已有配置合并到当前格式, 补齐所有默认值并写回文件.

```toml
version = 3

[output]
format = "auto"

[network]
timeout_seconds = 30

[download]
threads = 8

[safety]
read_only = false
confirm_dangerous_actions = true

[watch]
interval_seconds = 300
request_gap_ms = 400

[watch.notify]
webhook = ""
exec = ""
```

`output.format` 支持 `auto`, `rich`, `json`, `yaml`. `OUTPUT` 环境变量会覆盖这个值.

`download.threads` 控制媒体分段下载的并发线程数, 默认值为 `8`, 可设置为 `1` 到 `128`.

`safety.read_only = true` 会拒绝动态发布和删除, 点赞, 投币, 一键三连, 取关等账户侧写操作. `me login` 和 `me logout` 仍然可用. `confirm_dangerous_actions` 控制删除动态和取关是否需要额外确认.

视频信息, 媒体下载和 `bili watch` 都属于读取操作, 在只读模式下仍然可用.

## 订阅

```shell
bili watch add up 946974
bili watch add up 946974 --video --title-contains 直播回放
bili watch add up 946974 --live --live-end --live-title-change
bili watch add up 946974 --dynamic
bili watch add video BV1ABcsztEcY --like 10000 --view 100000
bili watch add search "关键词"
bili watch add list 7855491/8565435
bili watch list
bili watch check
bili watch run --interval 5m
bili watch disable 1
bili watch rm 1
```

`bili watch` 在本地轮询公开接口, 不依赖账号写操作. 默认 `add up` 同时订阅新视频和开播. 第一次检查只记录当前快照, 不会把历史上的视频或动态当成新事件; 视频数据阈值在当前值已经超过设定时会立即通知一次, 之后同一阈值不再重复触发.

`watch check` 适合放进 cron. `watch run` 按 `watch.interval_seconds` 或 `--interval` 常驻循环, 最短间隔 10 秒. 规则之间会等待 `watch.request_gap_ms`, 遇到限流时跳过本轮剩余规则. 动态订阅需要登录, 未登录只影响该条规则.

新事件会打印到终端. 配置 `watch.notify.webhook` 后会 POST 统一 envelope; `watch.notify.exec` 会对每条事件执行命令, 并注入 `BILI_WATCH_KIND`, `BILI_WATCH_TITLE`, `BILI_WATCH_URL`, `BILI_WATCH_SUMMARY` 和 `BILI_WATCH_JSON`.

## 输出

查询命令支持 `--json` 和 `--yaml`. 非交互 stdout 的表格默认输出 TSV, 其他查询命令默认输出 YAML. 成功和失败都使用统一结构:

```yaml
ok: true
schema_version: "1"
data: {}
```

## 搜索

```shell
bili search "关键词" --type video --order totalrank
bili search "关键词" --type article --order pubdate
bili search "关键词" --type bangumi --order click
```

`--type` 支持 `all`, `article`, `video`, `user`, `bangumi`, `live`, `media`.
省略 `--type` 时使用综合搜索, 也可以显式指定 `all`.
`--order` 支持 `totalrank`, `click`, `pubdate`, `dm`, `stow`.
表格在 PTY 中会按终端宽度对齐并截断可变长列. `--no-truncate` 可关闭截断. 非 PTY 的表格输出为未截断 TSV.

## 开发

```shell
go test ./...
just test
```

## 发布

发布版本以 `vX.Y.Z` 形式的 annotated tag 为唯一来源. 先为版本写入 `docs/changelog/X.Y.Z.md`, 再提交该文件并创建 tag. GitHub Actions 会校验 tag annotation 与说明文件完全一致, 只有六个平台产物和校验和都通过后才会创建或更新 GitHub Release.

```shell
git add docs/changelog/0.1.0.md
git commit -m "docs: add v0.1.0 release notes"
git tag -a "v0.1.0" --cleanup=verbatim -F "docs/changelog/0.1.0.md"
git push origin main
git push origin "v0.1.0"
```
