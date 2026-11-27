# 本地图片管理系统 需求方案文档

> 本文档面向 AI 开发，包含目标、数据模型、存储规则、导入流程、API 草案、前端页面、MVP 拆分和实现约束。开发时以本文档为准；文档未覆盖的细节，按“数据安全 > 简单可维护 > 性能 > 功能丰富”的优先级决策。

---

## 1. 项目概述

### 1.1 目标

构建一个**本地单用户、离线运行**的图片管理系统，解决现有文件夹管理在图片数量增大后浏览卡顿、查找困难、标签和相册管理缺失的问题。

核心特征：

- 图片导入后由系统**内容寻址存储（CAS）**，按 SHA-256 哈希分两级目录存放，物理文件只存一份。
- 元数据存本地 SQLite，数据库随 `./library` 一起备份。
- 支持标签、普通相册、智能相册、时间线浏览、文字搜索、批量操作、回收站。
- 后端 Go，前端 Vue 3 + Vite，浏览器访问本地服务。
- 不依赖第三方图片管理软件，不联网，不上传云端。

### 1.2 非目标（第一版不做）

- 多用户、权限、登录。
- 云端同步、远程访问。
- 人脸识别、OCR、语义搜索、以图搜图。
- 相似图检测（感知哈希）；只做完全重复（SHA-256 相同）。
- 评分、收藏、颜色标记。
- RAW、HEIC、AVIF、MKV/AVI 等格式；当前支持 JPG/JPEG/PNG/GIF/WebP(静态)/BMP/TIF/TIFF 与 MP4（视频只存原文件，可在查看器播放），其它格式仍走「不支持」报告。
- 图片编辑（裁剪、旋转、滤镜）。
- 自动清理回收站；回收站由用户手动清空。
- 系统托盘、Windows 服务、开机自启。
- 端口和存储路径的运行时配置；两者写死。

---

## 2. 技术栈与运行方式

| 层 | 选型 | 说明 |
|---|---|---|
| 后端 | Go | 单二进制，跨平台 |
| Web 框架 | 标准库 `net/http` | 本地服务，路由简单，标准库足够；不引入 Gin |
| SQLite 驱动 | `modernc.org/sqlite` | 纯 Go，免 CGO，便于交叉编译 |
| 图像处理 | `github.com/disintegration/imaging` + 标准库 `image/jpeg`、`image/png` | 缩略图生成、尺寸读取 |
| EXIF | `github.com/rwcarlsen/goexif/exif` 或同类纯 Go 库 | 读取拍摄时间、Orientation |
| 前端 | Vue 3 + Vite + TypeScript | 构建产物为静态文件，由 Go embed 提供 |
| 前端库 | PhotoSwipe（查看器）、`vue-virtual-scroller`（网格虚拟滚动） | 仅这两个第三方库，不引重型 UI 框架 |
| 运行方式 | 编译为单个 exe，双击启动，自动打开默认浏览器 `http://127.0.0.1:18765` | 控制台保留日志 |

### 2.1 固定约定

- **端口写死 `18765`**，绑定 `127.0.0.1`，不监听 `0.0.0.0`。端口冲突时启动失败并打印明确错误，不做自动换端口或配置。
- **存储路径写死**，相对于可执行文件所在目录：
  - `./library/`：需要备份的数据。
  - `./runtime/`：不需要备份的运行时产物，可随时删除重建。
- 首次启动时自动创建 `./library` 和 `./runtime` 及其子目录，无需用户配置。
- 不提供 `config.json`；少量运行时状态（上次导入路径等）存 `app_settings` 表。

### 2.2 目录布局

```text
<exe所在目录>/
  app.exe
  library/                    # 需要备份，整体复制即可
    blobs/
      ab/
        cd/
          abcd...ef.jpg
          IMG_001__abcd1234.jpg
    library.sqlite
    library.sqlite-wal
    library.sqlite-shm
  runtime/                    # 不需要备份，可删除重建
    thumbs/
      ab/
        cd/
          abcd...ef.jpg
    previews/                 # TIFF 等浏览器不可直显格式的 JPEG 预览
      ab/
        cd/
          abcd...ef.jpg
    trash/
      abcd...ef.jpg
    logs/
      app.log
    exports/
```

说明：

- `library/` 内只有原图和 SQLite 数据库，备份时直接复制整个目录即可，无需排除任何子目录。
- `runtime/` 内的缩略图、预览图、回收站、日志、导出副本均可重建或丢弃。
- 数据库的 WAL/SHM 文件位于 `library/` 内；停服后 SQLite 会 checkpoint 并清理 WAL/SHM，此时复制 `library/` 是安全的。

---

## 3. 存储布局

### 3.1 原图（library/blobs）

- 哈希为 SHA-256 小写十六进制 64 位。
- 一级目录取 `hash[0:2]`，二级目录取 `hash[2:4]`。
- 物理文件名优先使用原始文件名；同名冲突时追加 `__` + 哈希前 8 位，保留原扩展名。
- `blobs` 内文件视为不可变，不原地旋转、不压缩、不改 EXIF。
- 数据库 `assets.rel_path` 存相对 `library/blobs` 的路径，如 `ab/cd/xxx.jpg`。

### 3.2 缩略图（runtime/thumbs）

- 路径 `runtime/thumbs/ab/cd/<hash>.jpg`，用哈希命名，避免原名冲突。
- 长边 384px，JPEG，质量 80。
- 可随时删除重建，不参与备份。

### 3.3 回收站（runtime/trash）

- 文件名 `<hash>.<ext>`，不保留原名。
- 数据库 `assets` 保留全部字段，恢复时按 `storage_name` 和哈希重建 `blobs` 路径。
- 不参与备份；从备份恢复后，回收站中的记录可能因文件缺失而无法恢复，用户可直接清空。

### 3.4 日志与导出

- 日志 `runtime/logs/app.log`，按大小滚动，保留最近 10MB。
- 导出默认目录 `runtime/exports/`，用户可在导出时选择其他目标目录。

---

## 4. 数据模型（SQLite）

开启 `PRAGMA journal_mode=WAL;`、`PRAGMA foreign_keys=ON;`。

### 4.1 assets

```sql
CREATE TABLE assets (
  hash            TEXT PRIMARY KEY,
  original_name   TEXT NOT NULL,
  storage_name    TEXT NOT NULL,
  rel_path        TEXT NOT NULL,
  size            INTEGER NOT NULL,
  mime            TEXT NOT NULL,
  width           INTEGER,
  height          INTEGER,
  taken_at        TEXT,
  taken_at_source TEXT,
  taken_at_override TEXT,
  note            TEXT,
  added_at        TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'ready',
  deleted_at      TEXT
);
CREATE INDEX idx_assets_taken_at ON assets(taken_at);
CREATE INDEX idx_assets_status ON assets(status);
CREATE INDEX idx_assets_added_at ON assets(added_at);
```

说明：

- 不设 `title` 字段；图片没有标题，标题是相册属性。
- 不设评分、收藏字段。
- `taken_at` 存本地时间字符串 `YYYY-MM-DDTHH:MM:SS`，不带时区。
- 有效时间 = `COALESCE(taken_at_override, taken_at)`。
- `status` 取值：`importing` / `ready` / `trashed` / `missing`。
- 删除到回收站时，`rel_path` 保留原值作为恢复参考，`status='trashed'`，物理文件移到 `runtime/trash/<hash>.<ext>`。

### 4.2 tags / asset_tags

```sql
CREATE TABLE tags (
  id   INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE
);

CREATE TABLE asset_tags (
  hash   TEXT NOT NULL REFERENCES assets(hash) ON DELETE CASCADE,
  tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (hash, tag_id)
);
CREATE INDEX idx_asset_tags_tag ON asset_tags(tag_id);
```

标签扁平，无层级。名称 trim 后存储，大小写不敏感唯一。

### 4.3 albums / album_items

```sql
CREATE TABLE albums (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL,
  type        TEXT NOT NULL CHECK(type IN ('manual','smart')),
  description TEXT,
  rule_json   TEXT,
  cover_hash  TEXT,
  created_at  TEXT NOT NULL
);

CREATE TABLE album_items (
  album_id INTEGER NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
  hash     TEXT NOT NULL REFERENCES assets(hash) ON DELETE CASCADE,
  position INTEGER NOT NULL DEFAULT 0,
  added_at TEXT NOT NULL,
  PRIMARY KEY (album_id, hash)
);
CREATE INDEX idx_album_items_hash ON album_items(hash);
```

- 普通相册：`type=manual`，成员存 `album_items`。
- 智能相册：`type=smart`，成员不存表，按 `rule_json` 动态查询。
- 相册名、描述可搜索。

### 4.4 time_parse_rules

```sql
CREATE TABLE time_parse_rules (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  name     TEXT NOT NULL,
  pattern  TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 100,
  enabled  INTEGER NOT NULL DEFAULT 1,
  builtin  INTEGER NOT NULL DEFAULT 0
);
```

命名捕获组约定：`year`、`month`、`day`、`hour`、`minute`、`second`。缺省时分秒按 00。示例：

```regex
IMG_(?P<year>\d{4})(?P<month>\d{2})(?P<day>\d{2})_(?P<hour>\d{2})(?P<minute>\d{2})(?P<second>\d{2})
Screenshot_(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})-(?P<hour>\d{2})-(?P<minute>\d{2})-(?P<second>\d{2})
```

内置若干条常见规则，用户可在设置页增删改、调整优先级、测试匹配。

### 4.5 import_batches / import_items

```sql
CREATE TABLE import_batches (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  source_path TEXT NOT NULL,
  started_at  TEXT NOT NULL,
  finished_at TEXT,
  status      TEXT NOT NULL DEFAULT 'running',
  stats_json  TEXT
);

CREATE TABLE import_items (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id    INTEGER NOT NULL REFERENCES import_batches(id) ON DELETE CASCADE,
  source_path TEXT NOT NULL,
  hash        TEXT,
  status      TEXT NOT NULL,
  message     TEXT
);
```

- `import_items.status`：`new` / `duplicate_skip` / `duplicate_pending` / `duplicate_replace` / `failed`。
- 导入报告导出 CSV 后可清理 `import_items`；提供“清理历史”按钮。
- `import_batches` 保留最近记录，可清理。

### 4.6 app_settings

```sql
CREATE TABLE app_settings (
  key   TEXT PRIMARY KEY,
  value TEXT
);
```

存储：上次导入路径、回收站清空确认时间等少量状态。不存端口和 library 路径。

### 4.7 operations

```sql
CREATE TABLE operations (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  type       TEXT NOT NULL,
  payload    TEXT,
  created_at TEXT NOT NULL
);
```

第一版只记录关键操作（导入、删除、恢复、清空、导出），不实现撤销。

### 4.8 asset_signatures

```sql
CREATE TABLE asset_signatures (
  hash        TEXT NOT NULL REFERENCES assets(hash) ON DELETE CASCADE,
  algorithm   TEXT NOT NULL,
  value       TEXT NOT NULL,
  computed_at TEXT NOT NULL,
  PRIMARY KEY (hash, algorithm)
);
```

按 `(图片内容哈希, 算法)` 缓存指纹：当前只有 `dhash`，值为 16 位十六进制字符串。图片被永久删除时随 `assets` 级联删除。另为检测按有效时间排序/筛选新增表达式索引 `idx_assets_effective_time`（`COALESCE(taken_at_override, taken_at)`）。

---

## 5. 导入流程

### 5.1 触发方式

前端导入页手动输入本地绝对路径（方案 C），后端验证目录存在、可读，记住到 `app_settings`。不做浏览器上传，不做原生目录对话框。

### 5.2 递归扫描

- 递归遍历源目录**所有子目录**，不限制层级。
- 过滤支持的扩展名：`.jpg .jpeg .png .gif .webp .bmp .tif .tiff .mp4`（大小写不敏感；WebP 仅静态图有缩略图；MP4 只存原文件）。
- 跳过隐藏文件、系统文件、符号链接（避免循环）。
- 扫描结果先去重（同一路径只处理一次），再进入导入。
- **不支持的文件必须显式报告**：统计总数、按后缀（含「无后缀」）计数，并列出最多 100 个相对路径；扫描接口与导入进度/批次统计都返回该信息，CSV 报告也会追加 `unsupported` 行，避免文件被静默忽略。

### 5.3 导入流程

1. 用户输入源目录，点击“开始导入”。
2. 后端递归扫描源目录，得到待处理文件列表，创建 `import_batches`。
3. 对每个文件：
   1. 只读打开，计算 SHA-256。
   2. 查 `assets.hash`：
      - 已存在且 `original_name` 相同：标记 `duplicate_skip`，不复制。
      - 已存在且 `original_name` 不同：标记 `duplicate_pending`，加入重复列表，不复制。
      - 不存在：复制到 `library/blobs/ab/cd/`，复制后校验目标文件大小 == 源文件大小；不一致则删除目标、标记 `failed`。
   3. 读取图片尺寸、MIME、EXIF；按优先级解析时间；生成 `storage_name`（冲突加 `__hash8`）。
   4. 插入 `assets`，状态 `ready`。
   5. 写入 `import_items`。
4. 导入结束后，生成重复列表，前端展示对比（缩略图、文件名、大小），用户逐条或批量选择“跳过/替换”。
   - 替换：仅重命名项目内物理文件为新文件名，更新 `storage_name`、`original_name`；内容不复制。
5. 生成导入报告，可导出 CSV；用户确认后手动删除源目录。
6. 缩略图生成进入后台队列，不阻塞导入完成。

### 5.4 并发与中断

- 导入任务串行执行，同一时间只允许一个导入批次。
- 文件处理可并发（worker pool，建议 2–4 个 worker），数据库写入串行或加锁。
- 进程崩溃或强杀：启动时扫描 `assets.status='importing'` 的记录，校验文件存在且大小正确则置 `ready`，否则删除半文件并置 `failed`。
- 导入支持取消；取消后已完成文件保留，未完成标记 `cancelled`。

### 5.5 重复处理规则

- 哈希相同 + 文件名相同：自动跳过，不提示。
- 哈希相同 + 文件名不同：进入重复列表，默认“跳过”；用户可改“替换”。
- 替换只改项目内文件名，不复制内容，不碰源文件。
- 导入报告 CSV 字段：`source_path, hash, status, message`。

---

## 6. 缩略图

- 长边 384px，JPEG，质量 80。
- 路径 `runtime/thumbs/ab/cd/<hash>.jpg`。
- 后台队列异步生成，worker 数默认 2。
- 网格只加载缩略图；缩略图不存在时显示占位符并触发排队；生成失败（损坏文件、动图 WebP）在本次运行内不再重试，网格显示「无缩略图」。
- 缩略图可删除重建，不参与备份。
- EXIF Orientation 在缩略图阶段转正，不改原图。
- 大图查看读 `library/blobs` 原图；TIFF 浏览器无法直接渲染，查看器改读 `runtime/previews/ab/cd/<hash>.jpg`（长边 2560、质量 85，首次请求时生成并缓存）。
- 预览图同样是可删除重建的派生数据，不参与备份。
- MP4 视频：不在导入时转码，原文件进 CAS；缩略图由 `ffmpeg` 从 **已入库的 blob** 抽一帧（0.5s 处，失败回退第一帧）生成 384px JPEG。ffmpeg 是可选依赖：未安装时视频仍可导入/播放，网格显示「▶ 视频」占位。
- TIFF 由 `golang.org/x/image/tiff` 解码；个别压缩方式（如 JPEG-in-TIFF）暂不支持，这类文件仍可导入，但缩略图/预览会失败（网格显示「暂无缩略图」）。

---

## 7. 时间解析

优先级从高到低：

1. `taken_at_override`（手动覆盖）。
2. EXIF `DateTimeOriginal`，其次 `DateTimeDigitized`，再次 `DateTime`。
3. 文件名规则：按 `priority` 升序尝试 `time_parse_rules`，第一个匹配且日期合法的使用；`taken_at_source = filename_rule:<id>`。
4. 文件修改时间。
5. 导入时间，兜底。

结果存 `taken_at` 和 `taken_at_source`。解析失败不阻塞导入，`taken_at` 可空，UI 归“未知时间”。时间按本地时间存，不做时区转换。

---

## 8. 标签

- 扁平、多对多。
- 批量选择图片后：批量添加标签、批量移除标签。
- 标签管理页：列出所有标签及图片数，支持重命名、删除（级联移除关联）、合并（P1）。
- 侧边栏标签列表点击 = 打开该标签的临时筛选视图，不自动创建相册。
- 标签名搜索用 `LIKE '%kw%'`。

---

## 9. 相册与智能相册

### 9.1 普通相册

- 手动创建，填名称、描述。
- 从图库选择图片加入，或批量加入。
- 成员固定，手动增删。
- 支持设置封面（P1）。
- 相册详情页网格展示成员，支持导出。

### 9.2 智能相册

- 保存规则 AST，动态查询，成员随数据变化。
- 第一版规则支持：
  - 标签条件：`{"type":"tag","value":"旅行"}`
  - 时间范围条件：`{"type":"time_range","from":"2023-01","to":"2023-12"}`，`from`/`to` 可为 `YYYY` 或 `YYYY-MM`，闭区间
  - AND 组合：`{"op":"and","children":[...]}`
- 保留扩展：`or`、`not`、`rating`、`folder` 等节点类型；解析器遇到未知节点应报错并提示。
- 示例：

```json
{
  "op": "and",
  "children": [
    {"type": "tag", "value": "旅行"},
    {"type": "time_range", "from": "2023-01", "to": "2023-12"}
  ]
}
```

- 智能相册详情页展示动态结果，不提供手动增删成员。
- 相册名、描述可搜索。

### 9.3 侧边栏时间导航

- 自动聚合 `assets` 中有效时间非空的年月。
- 导航分两行：第一行是年份（含「全部」），第二行是所选年份的月份（含「全年」）；选项数从 `年 × 月` 降为 `年 + 月`，避免时间跨度大时按钮爆炸。
- 点击 = 临时筛选该年/该年月，不创建相册。
- 时间导航与标签筛选可组合：先点年月，再点标签，结果取 AND。
- 用户可把当前组合“另存为智能相册”。

---

## 10. 搜索

### 10.1 图片搜索

范围（默认任意字段）：

- `assets.original_name`
- `assets.note`
- `tags.name`（标签名）

SQL 用 `LIKE '%kw%'`（转义 `%`/`_`/`\`），`EXISTS` 子查询匹配标签。10k 量级足够，不引入 FTS5。

**搜索语法**（`internal/search`，前后端一致）：

- 空白即隐式 `AND`，也支持 `AND` / `OR`（大小写不敏感）。
- `NOT` 或前缀 `-` 取反，例如 `-截图`。
- 括号分组，例如 `(tag:旅行 OR tag:出差) AND NOT name:截图`。
- 引号短语，例如 `"beach day"`；引号内的 `AND/OR/NOT` 按字面值处理。
- 字段前缀：`name:`（同 `file:`）只匹配文件名、`note:` 只匹配备注、`tag:` 只匹配标签名；`tag:"长途 旅行"` 支持带空格的标签。
- 无法解析的表达式返回 400，并提示具体位置/原因。

未标记标签的图片可用 `untagged=1` 筛选；标签列表接口同时返回 `untagged_count` 供标签页固定首项展示。

结果排序：默认按有效时间倒序（与原实现一致），可用 `sort` 切换。

### 10.2 相册搜索

范围：`albums.name`、`albums.description`。

### 10.3 搜索交互

- 顶部搜索框，输入关键词，回车或防抖触发。
- 结果页分组或标签页展示“图片 / 相册”。
- 搜索可与标签、时间、相册筛选组合（AND）。

---

## 11. API 草案

统一前缀 `/api`，JSON。错误返回 `{"error":"message"}`，HTTP 状态码语义化。

### 11.1 图库与资产

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/assets` | 分页/筛选查询，参数：`q, tag, untagged, from, to, status, sort, page, page_size`（`q` 支持 10.1 的布尔语法） |
| GET | `/api/assets/{hash}` | 单张详情 |
| PATCH | `/api/assets/{hash}` | 更新 `note`、`taken_at_override`、`original_name`（可选） |
| POST | `/api/assets/{hash}/rename` | 重命名：同步数据库（original_name/storage_name/rel_path）与 CAS 文件 |
| POST | `/api/assets/{hash}/reveal` | 在系统文件浏览器中显示图片所在目录（macOS `open -R` / Windows `explorer /select,` / Linux `xdg-open`） |
| DELETE | `/api/assets/{hash}` | 软删除到回收站 |
| POST | `/api/assets/batch/tags` | 批量加/移除标签，body: `{hashes:[], add_tags:[], remove_tags:[]}` |
| POST | `/api/assets/batch/delete` | 批量软删除 |
| POST | `/api/assets/batch/albums` | 批量加入普通相册 |
| POST | `/api/assets/batch/export` | 批量导出，body: `{hashes:[], target_dir, by_album?}` |
| GET | `/api/assets/{hash}/file` | 返回原文件（图片或 MP4，支持 Range；`/preview` 用于 TIFF 等浏览器不可直显格式） |
| GET | `/api/assets/{hash}/thumb` | 返回缩略图 |
| GET | `/api/assets/{hash}/preview` | 返回查看器用 JPEG 预览（TIFF 等浏览器不可直显格式；其它格式回落原图），首次请求时生成并缓存 |

### 11.2 标签

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/tags` | 列表，含计数 |
| POST | `/api/tags` | 新建 |
| PATCH | `/api/tags/{id}` | 重命名 |
| DELETE | `/api/tags/{id}` | 删除 |
| POST | `/api/tags/merge` | 合并（P1） |

### 11.3 相册

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/albums` | 列表，支持 `q`、`type` |
| POST | `/api/albums` | 创建，body 含 `name, type, description, rule_json` |
| GET | `/api/albums/{id}` | 详情；smart 相册返回规则和动态结果（分页） |
| PATCH | `/api/albums/{id}` | 更新名称、描述、规则 |
| DELETE | `/api/albums/{id}` | 删除 |
| POST | `/api/albums/{id}/items` | 普通相册加成员 |
| DELETE | `/api/albums/{id}/items` | 普通相册移除成员 |

### 11.4 导入

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/import/scan` | 验证路径、递归统计文件数，body: `{path}`；返回 `total/unreadable/unsupported`（含后缀分布与最多 100 个文件） |
| POST | `/api/import/start` | 开始导入，body: `{path}`，返回 `batch_id` |
| GET | `/api/import/{batch_id}/progress` | 进度（轮询） |
| POST | `/api/import/{batch_id}/cancel` | 取消 |
| GET | `/api/import/{batch_id}/duplicates` | 重复列表 |
| POST | `/api/import/{batch_id}/duplicates/resolve` | 批量/单条解决，body: `{items:[{id, action:"skip"|"replace"}]}` |
| GET | `/api/import/{batch_id}/report.csv` | 导出 CSV |
| POST | `/api/import/{batch_id}/cleanup` | 清理 import_items |

### 11.5 时间、设置、回收站

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/timeline/months` | 有图片的年月列表及计数 |
| GET | `/api/settings` | 读取设置 |
| PUT | `/api/settings` | 更新设置（仅 app_settings 中的键） |
| POST | `/api/shutdown` | 关闭服务，body: `{confirm:true}`；先写回响应再优雅退出（取消导入 → 关闭 HTTP → 释放端口） |
| GET | `/api/trash` | 回收站列表 |
| POST | `/api/trash/restore` | 恢复，body: `{hashes:[]}` |
| POST | `/api/trash/empty` | 清空回收站 |
| GET | `/api/time-rules` | 时间解析规则列表 |
| POST/PATCH/DELETE | `/api/time-rules...` | 规则增删改 |
| POST | `/api/time-rules/test` | 测试正则，body: `{pattern, filename}` |
| GET | `/api/health` | 健康检查 |

### 11.6 相似图片检测

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/similar/detect` | 启动检测，body: `{from?, algorithm:"dhash", threshold?}`；`from` 为有效拍摄时间下界（可空），`threshold` 默认 8、范围 0–64 |
| GET | `/api/similar/progress` | 进度：`status/total/processed/groups/failures/cached/computed/current`；无任务时 `status=idle` |
| GET | `/api/similar/result` | 最近一次结果，组内图片带 `distance_from_prev`（组内第一张无该字段）；无结果返回 404 |
| POST | `/api/similar/cancel` | 取消进行中的检测；无任务返回 404 |

检测任务串行（同一时间只允许一个），结果仅保留在进程内存中。

分页统一参数：`page`（从 1 开始）、`page_size`（默认 100，最大 500）。返回 `{items:[], total, page, page_size}`。

---

## 12. 前端页面与交互

### 12.1 页面清单

1. **图库页**（默认首页）
   - 顶部搜索框、筛选栏（标签、时间、状态）。
   - 网格虚拟滚动，卡片显示缩略图、文件名、标签角标。
   - 批量选择：点击选中，Ctrl 切换，Shift 连选，全选当前筛选结果；底部批量操作栏（打标签、加相册、删除、导出）。
   - 点击图片打开查看器。
2. **时间线页**
   - 按年月分组，网格展示；左侧或顶部年月导航。
   - 支持与标签筛选组合。
3. **相册列表页**
   - 普通/智能分组，卡片显示封面、名称、描述、数量。
   - 新建相册（普通/智能）。
4. **相册详情页**
   - 普通：网格、添加/移除成员、导出。
   - 智能：展示规则、动态结果、编辑规则、导出。
5. **标签管理页**
   - 标签搜索（按名称实时筛选）、计数、重命名、删除。
   - 列表首项固定为「未标记任何标签」，点击跳到图库的 `untagged=1` 筛选，便于排查漏标图片。
6. **导入页**
   - 路径输入框（记住上次）、扫描预览（含递归统计与不支持文件明细）、开始导入、进度条、重复对比列表、报告导出。
   - 提示当前支持 jpg/jpeg/png/gif/webp/bmp/tif/tiff 与 mp4，以及「动图 WebP 暂无缩略图」「mp4 缩略图需要 ffmpeg」的限制。
7. **回收站页**
   - 列表、恢复、清空。
8. **设置页**
   - 只读展示 library 路径和端口。
   - 时间解析规则管理。
   - 清理导入历史。
   - 完整性校验入口。
   - 「关闭 iphoto 服务」：显式走优雅关闭（信号机制在 Windows 关闭窗口时不可靠）。
9. **相似图片检测页**
   - 起始时间（记住上次输入）、算法下拉（当前仅 dHash）、阈值（默认 8，0–64）。
   - 后台检测 + 进度条 + 取消；按有效时间排序后只比较相邻图片，连续相似成组，最多 100 组。
   - 结果按组展示缩略图、拍摄时间与相邻距离；点击缩略图查看该组原图；勾选后删除所选（默认全部保留，删除走回收站）。
10. **图片查看器**（覆盖层，非独立页面）
   - PhotoSwipe，支持上一张/下一张、缩放、全屏；最大缩放按适配尺寸放宽（约 8×，且至少 4× 原图）。
   - 缩放控件位于右上角工具栏，与 PhotoSwipe 自带的缩放、关闭按钮同排：`−` / `+` / `1:1`。
   - 键盘快捷键：`A` 上一张、`D` 下一张、`W` 放大、`S` 缩小、`Q` 退出、`G` 删除当前图片（移入回收站后自动切到下一张）；在输入框/备注等编辑区域中不触发。PhotoSwipe 自带的 Esc 关闭、左右方向键切换、`Z` 切换缩放仍然可用。
   - 侧栏显示文件名、路径、尺寸、时间、标签编辑、描述编辑。
   - 支持重命名（同步数据库与库内文件，扩展名不可修改）、在系统文件浏览器中显示所在目录。
   - 删除后自动切到下一张；已是最后一张时退出查看器。
   - TIFF 等浏览器无法直显的格式：`GET /api/assets/{hash}/preview` 返回服务端生成的 JPEG 预览图。
   - MP4 视频：slide 使用 `<video controls playsinline>`（PhotoSwipe 的 html 内容），沿用 `/file` 的 Range 拖动；切换/关闭时暂停；视频模式下隐藏缩放按钮并关闭垂直拖拽/方向键翻页，避免与原生控件冲突。

### 12.2 状态管理

- 用 Vue 3 `ref`/`reactive` + 简单 composable，不引入 Pinia（需要时再加）。
- 选择集：`const selected = ref(new Set<string>())`，提供 `toggle/has/clear/selectAll`。
- API 封装：`src/api/` 下按资源分文件，统一 fetch 封装、错误提示。
- 路由：Vue Router，页面级懒加载。

### 12.3 虚拟滚动

用 `vue-virtual-scroller` 的 `RecycleScroller` 或 `DynamicScroller`，网格按行虚拟化。若库不适合网格，改为自己实现窗口化，但优先用库。

加载策略：首屏分页（图库 200、时间线 300、相册 200），滚动到可视区最后一行时自动请求下一批（`scroll-end` + 视口未填满兜底），不再需要手动点「加载更多」。

### 12.4 前端构建

- Vite 构建产物 `dist/`，Go 用 `embed.FS` 嵌入。
- 生产：Go 提供静态文件 + SPA fallback 到 `index.html`。
- 开发：`npm run dev` + Go 后端独立运行，Vite proxy `/api` 到 `127.0.0.1:18765`。

---

## 13. 备份与校验

### 13.1 备份

- 停止服务。
- 确认 `library/` 下无 `library.sqlite-wal` 和 `library.sqlite-shm`（停服后 SQLite 会 checkpoint 并清理；若存在，说明未正常关闭）。
- 整体复制 `library/` 即可。`runtime/` 不备份。
- CAS 文件只增不改，适合增量备份。
- 恢复：停服，用备份覆盖 `library/`，启动。
- 回收站中的文件在 `runtime/trash/`，不备份；从备份恢复后，`status='trashed'` 的记录可能因文件缺失而无法恢复，用户可直接清空这些记录。

### 13.2 校验

设置页提供“完整性校验”按钮，手动触发：

- 遍历 `assets.status='ready'`，检查 `rel_path` 文件存在、大小一致；可选重算哈希。
- 扫描 `library/blobs`，找出数据库无记录的孤儿文件。
- 扫描 `assets`，找出文件缺失的记录，标记 `missing`。
- 对 `trashed` 记录单独处理：文件应在 `runtime/trash/`，缺失属正常（不备份），不报错。
- 输出报告，不自动删除，由用户决定。

### 13.3 从 CAS 重建

提供“重建基础索引”功能：扫描 `library/blobs`，重新计算哈希，重建 `assets` 基础字段。标签、相册、描述需数据库备份恢复；此功能只保证图片文件可被重新识别和浏览。

---

## 14. 回收站

- 删除 = `assets.status='trashed'`，`deleted_at` 记录时间，物理文件移到 `runtime/trash/<hash>.<ext>`，`rel_path` 保留原值作为恢复参考。
- 回收站页面列出，可恢复（移回 `library/blobs` 原路径，状态 `ready`）或清空（物理删除 `runtime/trash/` 文件并删除数据库记录）。
- 不设自动清理天数。
- 清空需二次确认。

---

## 15. 导出原图

- 入口：图库批量选择、相册详情、标签/时间筛选结果。
- 参数：目标目录（默认 `runtime/exports/`，可改）、是否按相册名建子目录。
- 行为：复制选中图片到目标目录，文件名用 `original_name`，冲突加 `__<hash8>`；不移动原库文件。
- 导出任务可后台执行，前端展示进度（P1 可简化为同步等待）。

---

## 16. MVP 拆分

### M1：存储与导入

- Go 项目骨架、SQLite schema、`library`/`runtime` 目录初始化、日志。
- CAS 存储、递归扫描、哈希、复制、大小校验、重复检测。
- 导入进度、重复列表、报告 CSV。
- 缩略图后台生成。
- 基础 API：assets 列表/详情/文件/缩略图、import。

### M2：浏览与查看

- Vue 项目骨架、路由、API 封装。
- 图库网格、虚拟滚动、筛选、搜索。
- 图片查看器、详情侧栏。
- 时间线页、年月导航。

### M3：标签与相册

- 标签 CRUD、批量打标签、标签筛选。
- 普通相册 CRUD、成员管理。
- 智能相册规则 AST、AND 标签 + 时间范围、动态查询。
- 侧边栏标签和时间导航。

### M4：回收站、导出、设置

- 回收站、恢复、清空。
- 导出原图。
- 时间解析规则管理。
- 完整性校验、孤儿扫描。
- 备份说明页。

### M5：打磨

- 错误处理、空状态、加载状态、快捷键。
- 性能检查（10k 张网格滚动、筛选响应）。
- 相似图片检测：dHash 指纹缓存、按时间相邻比对、结果页批量删除。
- 跨平台构建脚本。
- 文档：启动、导入、备份、恢复。

---

## 17. 实现约束与注意事项

1. **数据安全优先**：源文件只读，不移动、不改名、不删除；删除走回收站；清空和导出覆盖需确认。
2. **CAS 文件不可变**：入库后不修改原图；缩略图可重建。
3. **路径与端口写死**：`./library`、`./runtime` 相对于可执行文件目录；端口 `18765`。不做运行时配置。
4. **备份边界清晰**：`library/` 是唯一需要备份的目录；`runtime/` 可删除重建。
5. **数据库与文件一致性**：导入用 `status=importing` 中间态，启动时修复；不追求跨文件系统事务，接受最终一致。
6. **单用户本地**：不做鉴权、不做 CSRF 复杂防护，但绑定 `127.0.0.1`。
7. **性能目标**：10k 张图片，图库首屏 < 1s，滚动流畅，筛选响应 < 300ms（本地）。
8. **搜索简单实现**：`LIKE` 足够，不引入 FTS5。
9. **时间解析可配置**：内置常见规则 + 用户自定义正则，测试功能必备。
10. **智能相册规则可扩展**：AST 保留 `or`/`not`，第一版只实现 `and` + `tag` + `time_range`。
11. **前端不引重型 UI 框架**：只引 PhotoSwipe 和虚拟滚动库。
12. **Go 纯依赖**：SQLite 用 `modernc.org/sqlite`，图像处理避免 CGO，保证单二进制跨平台。
13. **递归导入**：扫描源目录时递归所有子目录，跳过符号链接和隐藏文件。
14. **日志**：关键操作写 `runtime/logs/app.log`，滚动保留最近 10MB。
15. **错误提示**：API 错误返回可读消息，前端 toast 展示；导入失败逐条记录，不整体回滚。
16. **测试重点**：哈希去重、递归导入、导入中断恢复、时间解析、智能相册查询、回收站恢复、导出文件名冲突。
17. **文档同步**：schema、API、规则 AST 变更时更新本文档或附带 `docs/` 说明。

---

## 18. 开发时确认的细节（不阻塞开工）

- 已支持 GIF/WebP(静态)/BMP/TIFF 与 MP4 视频；动图 WebP 因 `x/image/webp` 不支持动图解码，暂不生成缩略图（查看器仍可播放）。AVIF/HEIC 待后续阶段；MKV/AVI 等其它视频容器暂不支持。
- 普通相册封面：第一版自动取第一张，手动设置放 P1。
- 标签合并：P1。
- 导出任务进度：P1 可同步等待，P2 再做后台任务。
- 操作日志保留策略：按文件大小滚动。

---

## 19. 相似图片检测

### 19.1 目标与范围

- 找出「时间上相邻、内容相似」的图片（例如手机一次连拍的多张），而不是全局两两比较。
- 只检测 `status='ready'` 且有效拍摄时间（`taken_at_override` 优先）非空的图片；可指定起始时间（闭区间），无时间的图片不参与。
- 当前算法只有 `dhash`：9×8 灰度、横向相邻像素比较得到 64 位指纹，汉明距离 ≤ 阈值判为相似（默认 8，范围 0–64）。

### 19.2 指纹缓存

- 每张图片的指纹按 `(hash, algorithm)` 存入 `asset_signatures`；检测时优先复用，未命中才解码计算并立即入库。
- 计算来源：优先 384px 缩略图（已按 EXIF 转正），缩略图缺失/损坏时回退原图（`imaging.AutoOrientation`）。
- 计算失败（损坏文件等）计入 failures 并打断当前相似链，继续处理后续图片。

### 19.3 比对算法

1. 候选图片按有效时间升序（同时间按 added_at、hash）排序。
2. 顺序遍历：读取/计算指纹，与「前一张」求汉明距离。
3. 距离 ≤ 阈值则把当前图片追加到当前链；否则结束当前链（长度 ≥ 2 时产出一组），以当前图片开启新链。
4. 遍历结束后补上最后一条链。
5. 组数达到 100 立即停止整个任务并返回（剩余图片不再计算/比对）；已写入的指纹保留。

### 19.4 任务与接口

- 单实例、同一时间只允许一个检测任务；后台 goroutine 执行，前端每 400ms 轮询进度。
- 进度包含 total/processed/groups/failures/cached/computed/current；支持取消（返回已发现的部分结果）。
- 结果只保留在进程内存中（最近一次），不写入历史；服务重启后需重新检测。
- 起始时间在启动检测时写入 `app_settings.similar_from`，页面下次打开自动回填。

### 19.5 结果页

- 按组展示缩略图、文件名、拍摄时间与「与上一张 Δ距离」。
- 点击缩略图用现有查看器打开该组原图；每张图可勾选，默认全部保留。
- 「删除所选」调用现有批量删除接口移入回收站（可恢复）；删除后本地移除对应图片，剩余不足 2 张的组不再展示。

---

## 20. 视频（MVP：MP4）

### 20.1 范围

- 只支持 `.mp4`：原文件原样进入 CAS，不转码、不改变哈希/大小，导出、回收站、恢复、完整性校验与图片完全一致。
- 网格：优先用 ffmpeg 从入库后的 blob 抽一帧（0.5s 处，极短视频回退第一帧）生成 384px JPEG 缩略图；未安装 ffmpeg 时显示「▶ 视频」占位，不影响播放。
- 查看器：用 PhotoSwipe 的 html 内容渲染 `<video controls playsinline preload="metadata">`，`/api/assets/{hash}/file` 已支持 Range，可拖动进度。视频模式下隐藏缩放按钮、关闭垂直拖拽与方向键翻页；切换/关闭时暂停播放。
- 详情侧栏：类型显示「视频」，宽高留空（`NULL`），显示「—」；其它字段（标签、备注、时间覆盖、重命名、在文件浏览器中显示、删除）与图片一致。
- 不参与相似图片检测：`ListDetectionAssets` 明确排除 `mime LIKE 'video/%'`，避免解码失败打断相似链。

### 20.2 依赖与降级

- `ffmpeg` 为可选外部依赖（`PATH` 中查找）。未安装时：导入/播放正常，缩略图不可用；启动日志与 `/api/health` 的 `ffmpeg` 字段、设置页「运行信息」会提示。
- 浏览器能否播放取决于编码：H.264/AAC 的 MP4 兼容性最好；HEVC 等取决于平台/浏览器。页面会提示可用「在文件浏览器中显示」交给系统播放器。
- MKV/AVI 等容器暂不支持（仍出现在「不支持文件」报告里）；后续如需支持，优先考虑 MKV `-c copy` remux 到 MP4 或明确降级，而不是全量转码。

### 20.3 后续可选增强

- ffprobe 读取时长/宽高/旋转并写入 `assets`（需 schema 迁移）。
- 视频类型筛选、时长展示、客户端截帧作为无 ffmpeg 时的海报兜底。

---

## 21. 手机导入（PhoneDock）

### 21.1 目标与边界

手机端由独立项目 PhoneDock 提供能力（见 `../PhoneDock/README.md`）：设备信息、相册元数据（全量/增量）、原文件、缩略图、按需 SHA-256。iphoto 只作为调用方，**通过 HTTP 接口交互，不关心其实现**。

- 目标：在导入页填手机地址 + 4 位 Token，把手机相册增量拉进 `library/`；新设备从头读，老设备按上次进度续接。
- 手机侧**只读**：不修改、不删除手机内容；手机删图不会删 iphoto 的图（不做双向同步）。
- 不做：定时自动同步、断点续传（PhoneDock v1 不支持 Range）、手机端删除对账、HEIC/MOV 等新格式（仍按「类型不支持」显式报告）。
- 下载**串行**：PhoneDock 的原文件接口有传输锁（同一时刻只允许一个全文件读取），并发只会互相等待。

### 21.2 设备识别

- 身份：`device_key = lower(manufacturer) + "|" + lower(model)`，展示名 `manufacturer + " " + model`（如 `Xiaomi 22101317C`）。
- 不用地址做身份：手机 IP 会变（DHCP），按地址识别会让每次同步都变成「新设备全量」。
- 代价：两台同型号手机会命中同一条记录。检查连接时若地址与上次不同会提示，用户可勾选「全量重扫」（按 generation 重新列出，账本命中的不重传）或先「忘记设备」再全量。
- Token 明文存 `phone_devices.token`（局域网单用户）；页面可留空，后端按地址匹配已保存 Token。

### 21.3 数据模型（schema 003）

```sql
CREATE TABLE phone_devices (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  device_key      TEXT NOT NULL UNIQUE,
  name            TEXT NOT NULL,
  model           TEXT NOT NULL,
  manufacturer    TEXT NOT NULL,
  android_version TEXT NOT NULL,
  last_addr       TEXT NOT NULL,
  token           TEXT NOT NULL,
  created_at      TEXT NOT NULL,
  last_seen_at    TEXT NOT NULL,
  last_sync_at    TEXT,
  last_checkpoint INTEGER NOT NULL DEFAULT 0,   -- 上一轮成功读完的 album/version 水位
  last_status     TEXT,
  last_batch_id   INTEGER
);

CREATE TABLE phone_media (
  device_id           INTEGER NOT NULL REFERENCES phone_devices(id) ON DELETE CASCADE,
  kind                TEXT NOT NULL CHECK (kind IN ('image', 'video')),
  media_id            INTEGER NOT NULL,
  display_name        TEXT NOT NULL,
  generation_modified INTEGER NOT NULL,
  size                INTEGER NOT NULL,
  date_added          INTEGER NOT NULL,
  width               INTEGER,
  height              INTEGER,
  hash                TEXT,          -- 下载后才知道；内容寻址，与 assets.hash 对应
  status              TEXT NOT NULL, -- synced | failed | unsupported | missing
  message             TEXT,
  updated_at          TEXT NOT NULL,
  PRIMARY KEY (device_id, kind, media_id)
);

ALTER TABLE import_batches ADD COLUMN source TEXT NOT NULL DEFAULT 'local';
ALTER TABLE import_batches ADD COLUMN device_id INTEGER;
```

账本的作用：

1. **取消/中断后不重传**：checkpoint 不推进，下一轮重列同样的记录，账本命中的直接跳过。
2. **失败自愈**：`status='failed'` 的条目在每轮开头优先重试（最多 500 条），避免一条永久失败把 checkpoint 卡死。
3. **全量重扫只补不删**：generation 未变且资产仍在库的不下载；库内资产已删除的会重新拉一份。

### 21.4 同步流程

```text
POST /api/phone/sync
 1. GET /v1/device/info        → 规范化地址后登记/更新设备（保留 checkpoint）
 2. GET /v1/album/version      → V（本轮水位）
 3. since = 新设备或 mode=full ? 0 : device.last_checkpoint
 4. GET /v1/album/media?kind=image → 分页（500/页）；kind=video 同理
 5. 账本比对 → 待下载清单（含上一轮失败项）
 6. 逐项 GET /v1/album/media/{id}/content 流式下载到 runtime/tmp，边下边算 SHA-256
 7. 查 assets(hash)：已有 → duplicate_skip；没有 → CAS 落盘（rename）→ Inspect → 时间解析 → ready
 8. 两种 kind 都翻完且未取消 → last_checkpoint = V
```

- 探测（`/api/phone/probe`）只读：不登记设备、不写账本，失败不会留下空批次。
- 地址/Token/相册权限/能力开关在创建批次前就校验，错误直接返回中文原因（`2001/2002/2003/3002/3003` 都有对应文案）。
- 地址只允许 `http(s)://host:port`：拒绝路径、查询、用户信息；客户端不跟随重定向，避免 Token 被带到其他主机。
- 进度：`total` = 本轮要处理的条目数（含未变更），`unchanged` 单独计数；`bytes_total/bytes_done` 用于显示下载体积；`phase` 为 `listing` / `downloading`。

### 21.5 逐项处理与去重

| 情况 | 动作 |
|---|---|
| 扩展名不在白名单 | 不下载，计 `unsupported`，账本记 `unsupported` |
| 账本 `synced` 且 generation 未变、资产在库 | 跳过，计 `unchanged` |
| 账本 `synced` 但资产已被删 | 重新下载 |
| 账本 generation 变化 / 无记录 / `failed` | 下载（失败重试） |
| 账本 `missing`（手机侧 3001）且 generation 未变 | 跳过，不反复请求 |
| 下载后 hash 命中库内 ready/trashed | `duplicate_skip`，账本记 `synced` |

手机同步与本地导入共用同一套入库逻辑（`importer.ingest`）：查重、`importing` 中间态、大小校验、`media.Inspect`、时间解析、缩略图入队。差别只有两点：内容来源是「下载到临时文件后 rename 进 CAS」（避免大视频写两遍），重复内容一律自动跳过（手机是「补新」来源，不进「跳过/替换」列表）。

时间解析优先级不变：EXIF → 文件名规则 → **手机 `dateAdded`**（替代本地文件的 mtime）→ 导入时间；`taken_at_source` 仍记为 `file_mtime`。

`import_items.source_path` 用 `phone://<设备名>/<kind>/<id>/<文件名>`，CSV 报告与导入历史都能追溯来源；手机批次不会产生 `duplicate_pending`。未变更与类型不支持的条目不写 `import_items`（避免每轮几百行噪声，账本是它们的记录），因此 CSV 只列实际处理过（新增/重复/失败）的条目。

### 21.6 API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/phone/devices` | 设备列表（不含 Token）+ `counts`（synced/failed/unsupported/missing） |
| POST | `/api/phone/probe` | `{address, token}` → 设备信息、是否已知、`address_changed`、上次同步、`version`、两类媒体的 `total/pending` |
| POST | `/api/phone/sync` | `{address, token, mode, tags}` → `{batch_id}`；与本地导入互斥（忙则 409） |
| DELETE | `/api/phone/devices/{id}` | 忘记设备（删设备与账本，不动图片） |

进度/取消/CSV 报告复用 `/api/import/{batch}/*`；`ImportProgress` 增加 `source/device_name/phase/bytes_total/bytes_done/unchanged/checkpoint` 字段。

### 21.7 前端

- 导入页新增「从手机导入」面板（`PhoneImportPanel.vue`）：地址 + Token + 检查连接 + 标签 + 开始同步；检查通过前开始按钮不可用。
- 检查结果：设备名/Android 版本/电量、新设备的全量数量、已知设备的待同步数量与上次同步时间；`address_changed` 时提示并默认勾选「全量重扫」。
- 「已连接的手机」列表：增量同步 / 全量重扫 / 忘记；Token 未保存的禁用手动同步，提示重新填 Token。
- 进度面板复用：手机批次多显示设备名、下载体积、未变更数量、列表阶段；手机批次隐藏「刷新重复列表」。

### 21.8 异常与恢复

- 取消：结束当前下载、删 `.part`、批次 `cancelled`、**不推进 checkpoint**；已完成的条目保留。
- 进程崩溃：启动修复把 `running` 批次标 `interrupted`，并清理 `runtime/tmp/*.part`；checkpoint 未推进，下一轮重列，账本避免重传。
- 单项失败：`status='failed'` 记账本，下一轮自动重试；3001 记 `missing` 不再重试。
- 手机不可达/Token 变更/权限被关/能力关闭：probe 与 sync 都返回对应中文提示，不产生批次。
- 开发/验收小规模：环境变量 `IPHOTO_PHONE_ITEM_LIMIT=N` 让图片与视频**各**只处理前 N 项，且**不推进 checkpoint**（避免把截断误判为已同步）。

---

文档到此。开发时若遇到文档未覆盖的取舍，按 **数据安全 > 简单可维护 > 性能 > 功能丰富** 的优先级决策。
