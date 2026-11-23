# 开发环境与验证记录

> 首次验证：2026-11-22；M1–M3 完成后复验：2026-11-22 晚。
> 结论：环境可用，构建/测试/端到端流程均通过；唯一特殊处理是「工作区外缓存不可写」，已用项目内缓存目录解决。
> 沙箱与权限的**通用**方案已提取到工作区根目录的 [SANDBOX.md](../SANDBOX.md)，新项目可直接参考；本文只保留 iphoto 特有的环境信息与验证记录。

## 1. 环境信息

| 项 | 值 |
|---|---|
| 操作系统 | macOS 26.4.1（Darwin 25.4.0），arm64 |
| 工作区 | `/Users/lucas/WorkSpace/DeepseekSpace`（DSH 策略 `workspace-write`） |
| Go | 1.27.3 darwin/arm64；`GOPROXY=https://proxy.golang.com.cn,direct` |
| Node | **v26.13.1**（nvm；项目 `.nvmrc` = `26`） |
| npm | 11.20.0 |
| pnpm | 未安装（统一使用 npm） |
| git | 2.50.1 |
| 端口 | 后端 `18765`、Vite 开发 `5173` |

关键依赖：Go `modernc.org/sqlite v1.62.0`、`disintegration/imaging v1.6.2`、`rwcarlsen/goexif`；`go.mod` 要求 Go 1.26.0。
前端 `vue 3.5.46`、`vue-router 5.5.1`、`vite 8.3.8`、`@vitejs/plugin-vue 6.0.11`、`typescript 6.0.3`、`vue-tsc 3.3.14`、`photoswipe 5.4.4`、`vue-virtual-scroller 3.0.5`。

## 2. 权限边界与缓存方案

`workspace-write` 沙箱的限制、缓存重定向的环境变量、可复制的 `scripts/env.sh` 模板、新项目接入清单与常见坑，统一维护在工作区根目录的 [SANDBOX.md](../SANDBOX.md)。新项目按那份文档接入即可，不必重复排查。

iphoto 的具体落地：

- 缓存全部落在项目内 `iphoto/.cache/`（由 `scripts/env.sh` 重定向，已 gitignore）；`make` 内部会自动 source。
- 实测被拒路径（`operation not permitted`）：`~/go/pkg/mod`、`~/go/pkg/sumdb`、`~/Library/Caches/go-build`、`~/.npm`；`/tmp` 可写。
- `GOSUMDB` 保持开启，`go.sum` 校验不降级；缓存体积约 1.0GB，可 `make clean-cache` 清理后重建。
- `ps` 被禁（退出码 126），查端口用 `lsof -nP -iTCP:18765 -sTCP:LISTEN`。
- 沙箱内无法启动 Chrome，按项目约定不做浏览器自动化；UI 由用户在真实浏览器验收。

## 3. 验证记录

### 3.1 构建与静态检查

```text
go build ./...        PASS
go vet ./...          PASS
gofmt -l cmd internal web   无输出
go test ./...         PASS（34 个测试函数）
npm run typecheck     PASS（vue-tsc）
npm run build         PASS（vite，按路由/组件分包）
make build            PASS → bin/iphoto（约 11MB，内嵌前端）
```

测试覆盖：扫描过滤/CAS 命名冲突、时间解析优先级与非法日期、资产筛选与覆盖时间、标签 CRUD 与批量关联、普通相册成员、智能相册规则、导入端到端（去重/替换/取消）、启动中断修复、时间线聚合。

### 3.2 端到端（API，真实图片）

| 场景 | 结果 |
|---|---|
| 递归导入（含隐藏文件、符号链接、非图片） | 仅导入 jpg/png；3 新增 + 1 待处理重复 |
| 文件名时间规则 | `IMG_20230501_120000.jpg` → `2023-05-01T12:00:00`；`Screenshot_...` 命中内置规则 |
| 缩略图 | 后台生成，`GET .../thumb` 200 |
| 原图 Range | `Range: bytes=0-9` → 206 + `Content-Range` |
| 重复项替换 | 只重命名库内文件，`original_name/storage_name/rel_path` 同步更新 |
| 删除 | 文件移入 `runtime/trash/<hash>.<ext>`，状态 `trashed` |
| 重启 | 数据、状态、schema 均正常；WAL checkpoint 后无残留 |
| 时间线 | `/timeline/months` 正确聚合；覆盖时间参与聚合与筛选 |
| 标签 | 创建、重复创建 409、批量打标、按标签筛选、详情返回标签、重命名/删除 |
| 普通相册 | 创建、批量加成员、成员分页、移除 |
| 智能相册 | `and` + `tag` + `time_range` 动态命中；未知节点报错；成员不可手动添加 |
| 回收站 | 删除→列表→恢复（文件移回 rel_path）→再次删除→清空（记录+文件删除） |
| 导出 | 默认/指定目录导出，重复导出自动生成 `name__hash8.ext`，复制后大小校验 |
| 时间规则 | 新建→测试正则→禁用→删除 |
| 完整性校验 | 缺失文件标记 `missing`、孤儿文件识别、可选重算 SHA-256 |
| 交叉编译 | `CGO_ENABLED=0 GOOS=windows` 构建通过 |
| 时间预检 CLI | `iphoto-timecheck` 文本/JSON 输出、无数据库回退内置规则均正常 |
| 导出限制 | `subdir` 必填且禁止路径分隔符；只写入 `runtime/exports/<subdir>` |

### 3.3 M5 扩展功能复验（2026-11-23）

```text
gofmt -l cmd internal web   无输出
go vet ./...                PASS
go test ./...               PASS（新增 search 解析、搜索语法/untagged、扫描不支持文件、重命名等用例）
npm run typecheck           PASS
npm run build               PASS
make build                  PASS（内嵌前端）
```

API 端到端（临时 `IPHOTO_BASE_DIR=/tmp/iphoto-e2e`，脚本化验证）：

| 场景 | 结果 |
|---|---|
| 扫描混合目录（含 `.txt` / `.mp4` / `.heic` / 无后缀） | `unsupported.total=4`，按后缀计数正确，列出 4 个相对路径，`truncated=false` |
| 导入进度 / 重启后读取批次 | `unsupported` 统计存在且从 `stats_json` 恢复 |
| CSV 报告 | 追加 `unsupported` 行（绝对路径 + 后缀） |
| 搜索 `旅行` / `tag:旅行` / `NOT tag:旅行` / `-旅行` / `a OR b` / `(…) AND …` | 命中符合预期；默认搜索匹配标签名 |
| 非法表达式 `a AND` | HTTP 400 |
| `GET /api/assets?untagged=1`、`GET /api/tags` | 未标记筛选与 `untagged_count` 正确 |
| `POST /assets/{hash}/rename` | DB（original_name/storage_name/rel_path）与磁盘文件同步；改扩展名/非法字符返回 400 |
| `POST /assets/{hash}/reveal` | 通过 PATH shim 断言实际执行 `open -R <绝对路径>`；文件缺失返回 404 |
| `POST /api/shutdown` | 返回 `shutting_down` → 进程退出，日志含「收到页面关闭请求」「已退出」 |

UI（时间线两行导航、标签页搜索+固定首项、自动加载、查看器新控件）按项目约定仍由用户在真实浏览器验收。

2026-11-23 用户验收通过；随后按要求调整：缩放控件从左侧详情面板移到 PhotoSwipe 右上角工具栏（与自带缩放、关闭同排）。快捷键最终为 `A` 上一张 / `D` 下一张 / `W` 放大 / `S` 缩小 / `Q` 退出 / `G` 删除（Esc、方向键、`Z` 缩放等 PhotoSwipe 自带快捷键保留）。`npm run typecheck`、`npm run build` 复验通过，细节仍由用户浏览器验收。

相似图片检测（本轮新增模块）：

```text
gofmt -l cmd internal web   无输出
go vet ./...                PASS
go test ./...               PASS（新增 internal/similar：dHash、分组、缓存、上限、坏图、起始时间等 10 个用例，以及 v1→v2 迁移用例；共 55 个测试函数）
npm run typecheck           PASS
npm run build               PASS
```

临时库 API 端到端（7 张生成图片，mtime 决定拍摄时间；`IPHOTO_BASE_DIR=/tmp/...`）：

| 场景 | 结果 |
|---|---|
| 空结果 / 初始进度 | `GET /similar/result` → 404；`progress.status=idle` |
| 首次检测（阈值 8） | 7/7，组大小 `[3,2,2]`，组首图按时间排序，组内第一张无 `distance_from_prev` |
| 第二次检测 | `cached=7`、`computed=0`，分组一致 |
| 起始时间过滤 | 从 10:01 起 `total=6`，A 组只剩 a2/a3 |
| 起始时间持久化 | `app_settings.similar_from` 写入并可回读 |
| 参数校验 | 未知算法 / 阈值 65 均返回 400 |
| schema 与缓存 | `user_version=2`，`asset_signatures` 7 行 |
| 回收站联动 | 删除 a1 后再次检测 `total=6`，A 组剩 2 张 |

UI（相似图片页的进度、结果分组、勾选删除）由用户在真实浏览器验收。

### 3.4 浏览器验收

2026-11-22 首轮 M1 UI（图库 + 导入页）由用户在浏览器验收通过。
此后新增的 M2/M3/M4 界面（查看器、时间线、标签页、相册页/详情、回收站、设置页、批量操作）以 `typecheck` + `vite build` + API 端到端为准，**等待用户浏览器验收**。

### 3.5 新图片格式支持复验（2026-11-23，A 档）

新增 GIF/WebP（静态）/BMP/TIFF 支持；动图 WebP 按约定暂不生成缩略图（仅记录在案）。

```text
golang.org/x/image      0.0.0-2019... → v0.48.0（BMP/TIFF/WebP 解码 + 元数据标记修复）
gofmt -l cmd internal web   无输出
go vet ./...                PASS
go test ./...               PASS（新增 media 格式/缩略图/预览、扫描白名单、导入新格式等用例）
npm run typecheck           PASS
npm run build               PASS
```

API 端到端（临时 `IPHOTO_BASE_DIR=/tmp/iphoto-fmt-e2e`，脚本化验证：BMP/TIFF/静态 WebP/动图 GIF/动图 WebP + .txt）：

| 场景 | 结果 |
|---|---|
| 扫描 | `total=5`，`unsupported` 只剩 `.txt` 1 个 |
| 导入 | 5 个全部 `done`，MIME 分别为 image/bmp、image/tiff、image/webp、image/gif、image/webp，宽高均已写入 |
| 缩略图 | BMP/TIFF/静态 WebP/动图 GIF 均 200 + `image/jpeg`；动图 WebP 返回 404（预期，前端显示「无缩略图」） |
| 动图 WebP 原图 | `GET /file` 200 + `image/webp`（浏览器可播放） |
| TIFF 预览 | `GET /preview` 200 + `image/jpeg`，缓存到 `runtime/previews/ab/cd/<hash>.jpg`；二次请求复用缓存 |
| 非 TIFF 的 preview | 回落到原图（WebP 返回 `image/webp`） |
| 动图 GIF | 缩略图取首帧，查看器保留动画（浏览器原生） |
| 失败重试 | 缩略图生成失败后本进程内不再重复解码（`thumbs.Queue` 失败缓存） |

二进制（`-s -w -trimpath`）由 11,869,794 字节（11.3MB）增至 12,004,706 字节（11.4MB），约 +0.13MB（仅 x/image 解码器，无 wasm、无 CGO）；TIFF 预览首次生成 ~0.1s（320×240 样本），不阻塞导入。

UI（新格式网格缩略图、TIFF 查看器预览、动图 WebP「无缩略图」占位）由用户在真实浏览器验收。

## 4. 操作步骤

```bash
cd /Users/lucas/WorkSpace/DeepseekSpace/iphoto
source scripts/env.sh          # 每个新 shell 都要执行；make 内部已 source
make deps                      # Go 模块
make web-install               # 前端依赖
make build && make run         # 构建并运行（自动开浏览器）
```

分离开发：

```bash
make dev-backend               # 终端 A
make dev-web                   # 终端 B → http://127.0.0.1:5173
```

一键自检：`make verify`（构建 + 启动 + `/api/health` + 目录检查 + API 边界）。

数据隔离测试（不污染项目 `library/`）：

```bash
IPHOTO_BASE_DIR=/tmp/iphoto-test ./bin/iphoto -no-browser
```

清理：`make clean`（产物）、`make clean-cache`（工具链缓存）、`rm -rf library runtime`（本地数据）。

## 5. 注意事项

1. **Node 版本**：nvm 默认别名仍是 `22`，项目通过 `.nvmrc` 固定 `26`。交互式终端请 `nvm use` 或执行一次 `nvm alias default 26`，否则 `make build` 会用 Node 22。
2. **不要用 Chrome/无头浏览器做验证**：原因与替代方案见 [SANDBOX.md](../SANDBOX.md) 第 1、6 节；UI 验收由用户在真实浏览器完成。
3. **`gofmt` 需限定目录**：`.cache/` 内的模块 testdata 会被误扫；使用 `make fmt`（已限定 `cmd internal web`），详见 [SANDBOX.md](../SANDBOX.md) 第 6 节。
4. **Vite 显式绑定 IPv4**：`web/vite.config.ts` 设置 `host: '127.0.0.1'`，否则部分环境只监听 `::1`。
5. **`web/dist` 不提交**：仓库只保留 `.gitkeep` 以保证 `go:embed` 可编译；未构建前端时页面返回「前端未构建」提示。
6. **端口固定 18765**：被占用时启动失败并给出明确错误，不自动换端口。
7. **`IPHOTO_BASE_DIR` 仅用于开发**：正式运行时 `library/`、`runtime/` 位于可执行文件目录旁（dev.md 2.1）。
8. **启动修复策略**：`status='importing'` 的记录若文件大小正确则补全为 `ready`，否则删除半成品文件与记录（`assets.status` 无 `failed` 取值）。
