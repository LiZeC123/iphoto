# iPhoto

本地单用户、离线运行的图片管理系统。图片按 SHA-256 内容寻址存储（CAS），元数据存本地 SQLite，`library/` 整体备份即可迁移。

需求与设计以 [dev.md](./dev.md) 为准；环境与操作细节见 [ENVIRONMENT.md](./ENVIRONMENT.md)。

## 当前状态

**M1（存储与导入）、M2（浏览与查看）、M3（标签与相册）、M4（回收站/导出/设置）已完成；M5 打磨进行中。**
版本 `0.1.0`，单二进制内嵌前端，支持交叉编译。

已完成功能：

- **导入**：递归扫描、SHA-256 去重、CAS 两级目录、复制后大小校验、3 worker 并发、批次串行、取消、重复项（跳过/替换）、CSV 报告、启动时修复中断导入。
- **浏览**：图库网格（按行虚拟滚动）、文件名/描述搜索、年月范围筛选、排序、分页加载。
- **查看**：PhotoSwipe 查看器、上一张/下一张、缩放、键盘快捷键、详情侧栏（备注、拍摄时间覆盖、删除）。
- **时间线**：按年月聚合导航，点击年月筛选。
- **标签**：创建/重命名/删除、批量打标签、标签筛选、侧边栏快捷入口。
- **相册**：普通相册（成员增删、封面自动取首图）、智能相册（`and` + `tag` + `time_range` 动态查询）、相册搜索。
- **缩略图**：384px 后台队列，按需触发重建，按哈希命名可长期缓存。
- **回收站**：软删除、列表、批量恢复（文件移回原 rel_path）、清空（二次确认）。
- **导出**：批量/相册导出到 `runtime/exports/<名称>/`（只允许在导出目录下建子目录），文件名冲突自动加 `__hash8`，复制后校验大小。
- **设置**：运行信息与备份说明、时间解析规则增删改/启停/正则测试、完整性校验（缺失/大小/哈希/孤儿文件）、导入历史清理。
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
│   ├── importer/              # 扫描、哈希、CAS、导入管理器
│   ├── timeparse/             # EXIF → 文件名规则 → mtime → 导入时间
│   ├── thumbs/                # 缩略图后台队列
│   ├── maintenance/           # 启动时中断修复
│   └── httpapi/               # 全部 /api 路由 + SPA 托管
├── web/
│   ├── src/components/        # AssetGrid（虚拟滚动）、PhotoViewer、AssetThumb
│   ├── src/views/             # 图库/时间线/相册/详情/标签/导入/回收站/设置
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

## API 概览

统一前缀 `/api`，错误返回 `{"error":"..."}`。

| 资源 | 端点 |
|---|---|
| 资产 | `GET /assets`（q/tag/from/to/sort/page）、`GET|PATCH|DELETE /assets/{hash}`、`GET /assets/{hash}/file`（Range）、`GET /assets/{hash}/thumb`、`GET /assets/{hash}/tags`、`POST /assets/batch/tags`、`POST /assets/batch/delete` |
| 标签 | `GET|POST /tags`、`PATCH|DELETE /tags/{id}`、`POST /tags/summary`（所选图片的标签覆盖统计） |
| 相册 | `GET|POST /albums`、`GET|PATCH|DELETE /albums/{id}`、`GET|POST|DELETE /albums/{id}/items`（支持 `from`/`to`）、`GET /albums/{id}/months` |
| 回收站/导出 | `GET /trash`、`POST /trash/restore|empty`、`POST /assets/batch/export`（`subdir` 必填，导出到 `runtime/exports/<subdir>`） |
| 时间规则 | `GET|POST /time-rules`、`PATCH|DELETE /time-rules/{id}`、`POST /time-rules/test` |
| 导入 | `POST /import/scan|start`（`start` 可带 `tags`）、`GET /import/active`、`GET /import/{id}/progress`、`POST /import/{id}/cancel`、`GET /import/{id}/duplicates`、`POST /import/{id}/duplicates/resolve`、`GET /import/{id}/report.csv`、`POST /import/{id}/cleanup` |
| 其他 | `GET /timeline/months`、`GET|PUT /settings`、`POST /settings/verify`、`GET /import/batches`、`GET /health` |

分页统一 `{items,total,page,page_size}`；`page_size` 默认 100、上限 500。

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
