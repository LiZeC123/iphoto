# iPhoto

本地单用户、离线运行的图片管理系统。图片按 SHA-256 内容寻址存储（CAS），元数据存本地 SQLite，`library/` 整体备份即可迁移。

需求与设计以 [dev.md](./dev.md) 为准；环境与操作细节见 [ENVIRONMENT.md](./ENVIRONMENT.md)。

## 当前状态

**M1（存储与导入）、M2（浏览与查看）、M3（标签与相册）、M4（回收站/导出/设置）已完成；M5 打磨进行中；另外新增了「从手机（PhoneDock）增量导入」。**
版本 `0.1.0`，单二进制内嵌前端，支持交叉编译。

已完成功能：

- **导入**：递归扫描、SHA-256 去重、CAS 两级目录、复制后大小校验、3 worker 并发、批次串行、取消、重复项（跳过/替换）、CSV 报告、启动时修复中断导入；**不支持的文件会被显式报告**（总数、按后缀计数、最多 100 个文件明细，CSV 同步）。支持 jpg/jpeg/png/gif/webp/bmp/tif/tiff/avif/heic/heif 与 mp4。
- **手机导入（PhoneDock）**：局域网内的手机装上 PhoneDock 后，在导入页填地址 + 4 位 Token 即可拉取相册；设备按「厂商+型号」识别，首次全量、之后按 `GENERATION_MODIFIED` 水位增量；同步账本保证取消/重扫不重复传输，失败项自动重试；手机侧只读，删除不影响库内图片。
- **浏览**：图库网格（按行虚拟滚动）、文件名/描述/**标签**搜索（支持 `AND`/`OR`/`NOT`、括号、引号短语与 `name:`/`note:`/`tag:` 字段前缀）、年月范围筛选、排序、**滚动到底部自动加载**。
- **查看**：PhotoSwipe 查看器、上一张/下一张、**进一步放大（右上角工具栏 `−` / `+` / `1:1`）**、快捷键（`A` 上一张、`D` 下一张、`W` 放大、`S` 缩小、`Q` 退出、`G` 删除）、详情侧栏（备注、拍摄时间覆盖、**重命名（同步数据库与文件）**、**在系统文件浏览器中显示**、删除后**自动切到下一张**，最后一张删除后退出）；TIFF 与 HEIC/HEIF 由服务端转成 JPEG 预览后显示（AVIF 浏览器原生支持）。
- **视频（MVP）**：mp4 原文件入库（哈希/校验/导出/回收站与图片一致），查看器内用原生 `<video>` 播放（支持 Range 拖动）；网格缩略图由 ffmpeg 抽帧（可选依赖，未安装时占位）；视频不参与相似图片检测，MKV 等容器仍按「不支持」报告。
- **时间线**：按年/月两行聚合导航（选项数从 m×n 降为 m+n），点击年或年月筛选。
- **标签**：标签搜索、首项固定「未标记任何标签」快速排查漏标、重命名/删除、批量打标签、标签筛选、侧边栏快捷入口。
- **相似检测**：dHash 指纹按内容哈希缓存（`asset_signatures`，重算不重复解码）、按有效时间排序后只比较相邻图片、连续相似链式分组、进度/取消、最多 100 组；结果页可查看原图并勾选移入回收站（默认全部保留）。
- **相册**：普通相册（成员增删、封面自动取首图）、智能相册（`and` + `tag` + `time_range` 动态查询）、相册搜索。
- **缩略图**：384px 后台队列，按需触发重建，按哈希命名可长期缓存；动图 GIF / 静态 WebP / BMP / TIFF / AVIF / HEIC 均可用（动图 WebP 暂不生成缩略图）；MP4 由 ffmpeg 抽一帧，未安装 ffmpeg 时显示「▶ 视频」占位。
- **回收站**：软删除、**缩略图网格**（卡片显示文件名、删除时间与大小，点击选中/Shift 连选）、批量恢复（文件移回原 rel_path）、清空（二次确认）。
- **导出**：批量/相册导出到 `runtime/exports/<名称>/`（只允许在导出目录下建子目录），文件名冲突自动加 `__hash8`，复制后校验大小。
- **设置**：运行信息与备份说明、时间解析规则增删改/启停/正则测试、完整性校验（缺失/大小/哈希/孤儿文件）、导入历史清理、**手动关闭服务（优雅退出，跨平台）**。
- **运维**：`/api/health`、10MB 滚动日志、固定端口/路径、源文件只读、跨平台构建（`make release`）。

## 技术栈

| 层 | 选型 |
|---|---|
| 后端 | Go 1.26+，标准库 `net/http`（Go 1.22+ 路由模式） |
| 数据库 | SQLite（`modernc.org/sqlite`，无 CGO），WAL + 外键 |
| 图像 | `disintegration/imaging` + `rwcarlsen/goexif`；AVIF/HEIC 由 `gen2brain/gav1d`、`gen2brain/h265` 纯 Go 解码 |
| 前端 | Vue 3.5 + Vite 8 + TypeScript 6 |
| 查看器 / 虚拟滚动 | PhotoSwipe 5、vue-virtual-scroller 3 |
| 打包 | Vite 产物 `go:embed` 进单二进制 |

## 目录结构

```text
iphoto/
├── dev.md                     # 需求方案文档（唯一需求来源）
├── ENVIRONMENT.md             # 环境、权限与验证记录
├── Makefile                   # 构建/运行/自检入口
├── cmd/app/main.go            # 启动：目录 → 日志 → DB → 修复 → HTTP
├── internal/
│   ├── config/                # 端口、目录常量与路径解析
│   ├── logging/               # slog + 按大小滚动
│   ├── store/                 # SQLite：assets/tags/albums/imports/phone 设备与账本
│   ├── importer/              # 扫描、哈希、CAS、导入管理器、重命名、手机同步
│   ├── phonedock/             # PhoneDock v1 HTTP 客户端（信封、错误码、分页、下载）
│   ├── search/                # 图库搜索表达式解析（AND/OR/NOT/括号/字段前缀）
│   ├── similar/               # 相似图片检测：dHash、指纹缓存、相邻比对
│   ├── timeparse/             # EXIF → 文件名规则 → mtime → 导入时间
│   ├── thumbs/                # 缩略图后台队列
│   ├── maintenance/           # 启动时中断修复
│   └── httpapi/               # 全部 /api 路由 + SPA 托管
├── web/
│   ├── src/components/        # AssetGrid（虚拟滚动+自动加载）、PhotoViewer、AssetThumb、UnsupportedReport、PhoneImportPanel
│   ├── src/views/             # 图库/时间线/相似图片/相册/详情/标签/导入/回收站/设置
│   └── dist/.gitkeep          # 占位；make web-build 生成真实产物
└── scripts/{env,verify-env}.sh
```

运行期数据（均已 `.gitignore`）：`library/`（**唯一需要备份**）、`runtime/`（可删，含缩略图、预览图、回收站、日志、导出）、`.cache/`（工具链缓存）、`bin/`（产物）。

## 快速开始

```bash
source scripts/env.sh      # 必需：把 Go/npm 缓存指向项目内 .cache/
make deps                  # 下载 Go 模块
make web-install           # 安装前端依赖（Node 26，见 .nvmrc）
make build                 # 前端构建 + 嵌入二进制 → bin/iphoto
make run                   # 以项目根为数据目录运行，自动开浏览器
```

开发模式（两个终端）：

```bash
make dev-backend           # 127.0.0.1:18765
make dev-web               # http://127.0.0.1:5173（/api 代理到后端）
```

首次使用：打开「导入」页，输入本地照片目录绝对路径 → 扫描 → 开始导入。支持 jpg/jpeg/png/gif/webp/bmp/tif/tiff/avif/heic/heif 与 mp4，递归子目录，跳过隐藏文件与符号链接。动图 WebP 可导入和查看，但暂时没有网格缩略图（需要后续接入 libwebp）。MP4 原样入库、可在查看器中播放；缩略图依赖系统安装 ffmpeg（未安装时显示 ▶ 占位，不影响播放）。

> 视频缩略图需要 `PATH` 中能找到 `ffmpeg`（macOS 可 `brew install ffmpeg`，Windows 可下载官方 build 并加入 PATH）；未安装时 MP4 仍可导入和播放，设置页「运行信息」会显示 ffmpeg 状态。

## 从手机导入（PhoneDock）

手机端安装并打开 [PhoneDock](../PhoneDock/README.md)，点「启动服务」，按屏幕上的地址与 4 位 Token 在导入页填写即可。

1. 导入页 →「从手机导入」→ 填地址（如 `192.168.1.215:8765`，可省略 `http://`）与 Token。
2. 点「检查连接」：显示设备名、Android 版本、图片/视频总数与本次待同步数量；**新设备会提示将从头读取**，已知设备显示上次同步时间。
3. 选可选的导入标签 →「开始同步」。进度、取消、CSV 报告与本地导入共用下面那块批次面板。

行为说明：

- **设备识别**：按「厂商 + 型号」识别，手机换 IP 只更新地址，增量进度继续；同型号的第二台手机会命中同一条记录，检查结果会提示「地址与上次不同」，可勾选「全量重扫」或先「忘记」该设备。
- **增量**：每轮开始取一次 `/v1/album/version` 作为水位，图片与视频都翻完后才把水位记成下一轮的 checkpoint；取消、鉴权失败、网络中断都不会推进进度。
- **同步账本**（`phone_media`）：记录每个媒体已同步的哈希与 generation，取消后重扫、全量重扫都不会重复下载已入库内容；上一轮失败的会在一轮开头自动重试（最多 500 条）。
- **去重**：仍以 SHA-256 内容寻址；手机上的图片若与库内已有内容相同（含不同文件名）自动跳过并计入「库内已有」。
- **删除以 PC 为准**：同步过一次的图片，之后在 iphoto 里删除（含清空回收站）**不会**被手机同步重新导入，即便手机上的原图还在、即便再点「全量重扫」。要重新导入只能「忘记设备」这个显式重置入口（会清掉该手机的同步记录与账本）。
- **时间**：EXIF → 文件名规则 → 手机的 `dateAdded` → 导入时间（手机上拍照有 EXIF 时以 EXIF 为准）。
- **格式**：与本地导入同一白名单（含 AVIF/HEIC）；MOV/RAW 等其余格式会显式报告为「类型不支持」，不下载。
- **只读**：手机侧只读；手机删除图片不会从 iphoto 删除（保持库内不变）。下载串行（PhoneDock 原文件接口有传输锁、不支持 Range），首次全量就是按体积顺序走一遍。

相关接口：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/phone/devices` | 已登记设备（不含 Token）+ 同步账本统计 |
| POST | `/api/phone/probe` | 检查连接与待同步数量，不写库 |
| POST | `/api/phone/sync` | `{address,token,mode:"incremental"\|"full",tags}` → `batch_id` |
| DELETE | `/api/phone/devices/{id}` | 忘记设备（删同步记录，不动图片） |

Token 以明文保存在 `library.sqlite` 的 `phone_devices.token`（局域网单用户场景，换取的是一键增量同步的便利）；`library/` 备份会一并带走。

## AVIF / HEIC

- 支持 `.avif`、`.heic`、`.heif`：原文件入库（CAS 不转码），网格 384px 缩略图照常生成。
- 查看器：TIFF 与 HEIC/HEIF 由服务端转成 2560px JPEG 预览（首次打开时生成、按哈希缓存）；AVIF 由浏览器直接显示。
- 解码用两个纯 Go 库（`gen2brain/gav1d`、`gen2brain/h265`），无 CGO，`make release` 的 darwin/linux/windows 目标仍可交叉编译；二进制约 +3MB。
- 方向与尺寸：缩略图、预览与详情尺寸都会应用容器旋转（iPhone 竖拍照片正常竖显），旧版本导入的 AVIF/HEIC 会在启动时自动修复一次；仅在 EXIF 里写方向、容器无 transform 的非标准 HEIC 可能不自动转正。
- 其它限制：HEIC 图像序列按主图处理；解码器不支持的 HEVC 特性会拒绝解码，该文件记为导入失败。
- HEIC 涉及 HEVC 专利、AVIF 涉及 AV1 专利：许可与声明见 [THIRD-PARTY-NOTICES.md](./THIRD-PARTY-NOTICES.md)。

## 搜索语法

图库搜索框在文件名、备注、标签名中做子串匹配，并支持布尔组合：

| 写法 | 含义 |
|---|---|
| `旅行 海边` / `旅行 AND 海边` | 同时满足（空白即隐式 AND） |
| `旅行 OR 海边` | 任一满足 |
| `NOT 截图` / `-截图` | 取反 |
| `(tag:旅行 OR tag:出差) AND NOT name:截图` | 括号分组 |
| `tag:"长途 旅行"` | 字段 + 引号短语 |

字段前缀：`name:`（同 `file:`）、`note:`、`tag:`；不写时匹配三者。关键字大小写不敏感，若要把 `AND`/`OR`/`NOT` 当普通词搜索请加引号。表达式无法解析时接口返回 400，页面会直接显示原因。

## 相似图片检测

侧边栏「相似图片」页按时间顺序只比较相邻图片的 dHash，用来找连拍/连续相似的图片：

- 可选起始时间（只检测此时间之后的图片，留空 = 全部），算法当前只有 dHash，阈值默认 8（0–64，越小越严格）。
- 指纹按 `(内容哈希, 算法)` 缓存在 `library/library.sqlite` 的 `asset_signatures`，重复检测不再解码图片。
- 连续相似的图片归为一组；组数达到 100 时提前停止并返回。
- 结果按组展示缩略图、拍摄时间与相邻距离；点击缩略图查看原图；勾选后「删除所选」移入回收站（默认全部保留）。

## 导入前预检时间解析

在正式导入前，可以用小工具确认每个文件最终会被解析成什么时间（与导入使用完全相同的规则和扫描过滤）：

```bash
make timecheck
./bin/iphoto-timecheck -db library/library.sqlite /path/to/photos
# JSON 输出： ./bin/iphoto-timecheck -db library/library.sqlite -json /path/to/photos
```

- 规则从 `library.sqlite` 的 `time_parse_rules`（enabled=1）读取；数据库不存在时回退到内置规则并给出提示。
- 输出列：解析时间、来源（`exif:*` / `filename_rule:<id>` / `file_mtime` / `import`）、文件名。
- 来源为 `import` 的行会标 `⚠`，并在末尾汇总，提示这些图片会兜底到导入时间（通常意味着需要补规则）。

## 常用命令

| 命令 | 说明 |
|---|---|
| `make build` / `make run` | 构建单二进制 / 构建并运行 |
| `make dev-backend` / `make dev-web` | 分离开发模式 |
| `make typecheck` / `make test` / `make vet` / `make fmt` | 质量检查 |
| `make verify` | 一键环境自检（构建 + 启动 + 健康检查） |
| `make timecheck` | 构建时间解析预检工具 `bin/iphoto-timecheck` |
| `make release` | 交叉编译 darwin(arm64/amd64)、linux、windows 到 `dist/` |
| `make clean` / `make clean-cache` | 清理产物 / 清理工具链缓存 |

## 固定约定

- 监听 `127.0.0.1:18765`，冲突即启动失败，不自动换端口。
- 不提供 `config.json`；少量状态存 `app_settings`。
- `library/` 是唯一需要备份的目录（含手机设备的 Token 与同步账本）；`runtime/` 可随时删除（含手机导入临时文件 `runtime/tmp/`）。
- 源图片只读，删除走回收站，CAS 文件不可变。

## 下一步（M5）

错误态/空状态/加载态打磨、键盘快捷键、10k 张规模下的性能检查、跨平台构建与启动/备份/恢复文档整理；P1 项：标签合并、相册封面手动设置、导出任务进度。
