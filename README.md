# iPhoto

本地单用户、离线运行的图片管理系统。图片按 SHA-256 内容寻址存储（CAS），元数据存本地 SQLite，`library/` 整体备份即可迁移。

需求与设计以 [dev.md](./dev.md) 为准；环境与操作细节见 [ENVIRONMENT.md](./ENVIRONMENT.md)。

## 当前状态

**M1（存储与导入）、M2（浏览与查看）、M3（标签与相册）、M4（回收站/导出/设置）已完成；M5 打磨进行中。**
版本 `0.1.0`，单二进制内嵌前端，支持交叉编译。

已完成功能：

- **导入**：递归扫描、SHA-256 去重、CAS 两级目录、复制后大小校验、3 worker 并发、批次串行、取消、重复项（跳过/替换）、CSV 报告、启动时修复中断导入；**不支持的文件会被显式报告**（总数、按后缀计数、最多 100 个文件明细，CSV 同步）。
- **浏览**：图库网格（按行虚拟滚动）、文件名/描述/**标签**搜索（支持 `AND`/`OR`/`NOT`、括号、引号短语与 `name:`/`note:`/`tag:` 字段前缀）、年月范围筛选、排序、**滚动到底部自动加载**。
- **查看**：PhotoSwipe 查看器、上一张/下一张、**进一步放大（右上角工具栏 `−` / `+` / `1:1`）**、快捷键（`A` 上一张、`D` 下一张、`W` 放大、`S` 缩小、`Q` 退出、`G` 删除）、详情侧栏（备注、拍摄时间覆盖、**重命名（同步数据库与文件）**、**在系统文件浏览器中显示**、删除后**自动切到下一张**，最后一张删除后退出）。
- **时间线**：按年/月两行聚合导航（选项数从 m×n 降为 m+n），点击年或年月筛选。
- **标签**：标签搜索、首项固定「未标记任何标签」快速排查漏标、重命名/删除、批量打标签、标签筛选、侧边栏快捷入口。
- **相似检测**：dHash 指纹按内容哈希缓存（`asset_signatures`，重算不重复解码）、按有效时间排序后只比较相邻图片、连续相似链式分组、进度/取消、最多 100 组；结果页可查看原图并勾选移入回收站（默认全部保留）。
- **相册**：普通相册（成员增删、封面自动取首图）、智能相册（`and` + `tag` + `time_range` 动态查询）、相册搜索。
- **缩略图**：384px 后台队列，按需触发重建，按哈希命名可长期缓存。
- **回收站**：软删除、列表、批量恢复（文件移回原 rel_path）、清空（二次确认）。
- **导出**：批量/相册导出到 `runtime/exports/<名称>/`（只允许在导出目录下建子目录），文件名冲突自动加 `__hash8`，复制后校验大小。
- **设置**：运行信息与备份说明、时间解析规则增删改/启停/正则测试、完整性校验（缺失/大小/哈希/孤儿文件）、导入历史清理、**手动关闭服务（优雅退出，跨平台）**。
- **运维**：`/api/health`、10MB 滚动日志、固定端口/路径、源文件只读、跨平台构建（`make release`）。

## 技术栈

| 层 | 选型 |
|---|---|
| 后端 | Go 1.26+，标准库 `net/http`（Go 1.22+ 路由模式） |
| 数据库 | SQLite（`modernc.org/sqlite`，无 CGO），WAL + 外键 |
| 图像 | `disintegration/imaging` + `rwcarlsen/goexif` |
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
│   ├── store/                 # SQLite：assets/tags/albums/imports/smart 规则
│   ├── importer/              # 扫描、哈希、CAS、导入管理器、重命名
│   ├── search/                # 图库搜索表达式解析（AND/OR/NOT/括号/字段前缀）
│   ├── similar/               # 相似图片检测：dHash、指纹缓存、相邻比对
│   ├── timeparse/             # EXIF → 文件名规则 → mtime → 导入时间
│   ├── thumbs/                # 缩略图后台队列
│   ├── maintenance/           # 启动时中断修复
│   └── httpapi/               # 全部 /api 路由 + SPA 托管
├── web/
│   ├── src/components/        # AssetGrid（虚拟滚动+自动加载）、PhotoViewer、AssetThumb、UnsupportedReport
│   ├── src/views/             # 图库/时间线/相似图片/相册/详情/标签/导入/回收站/设置
│   └── dist/.gitkeep          # 占位；make web-build 生成真实产物
└── scripts/{env,verify-env}.sh
```

运行期数据（均已 `.gitignore`）：`library/`（**唯一需要备份**）、`runtime/`（可删）、`.cache/`（工具链缓存）、`bin/`（产物）。

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

首次使用：打开「导入」页，输入本地照片目录绝对路径 → 扫描 → 开始导入。支持 jpg/jpeg/png，递归子目录，跳过隐藏文件与符号链接。

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
- `library/` 是唯一需要备份的目录；`runtime/` 可随时删除。
- 源图片只读，删除走回收站，CAS 文件不可变。

## 下一步（M5）

错误态/空状态/加载态打磨、键盘快捷键、10k 张规模下的性能检查、跨平台构建与启动/备份/恢复文档整理；P1 项：标签合并、相册封面手动设置、导出任务进度。
