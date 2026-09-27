# Vocalis · 有声书播客服务器

把 NAS 上的有声书变成一个 **播客订阅源**，用 iPhone / iPad 自带的「播客」App
（或任意播客客户端）收听：整个书架一个订阅，想听哪本书就切到哪一「季」，
想听哪一集就直接点哪一集，播完自动接下一集，进度自动记住。

* 单个 Go 静态二进制 + Docker，镜像很小，群晖 / 威联通 / 绿联 / 极空间都能跑
* 只读挂载有声书目录，不改动你的文件
* 自动识别封面、ID3/M4A 标签、时长
* 可选在线刮削（Apple Books / Google Books）补全书名、作者、简介、封面
* 纯 Web 界面，支持手机浏览器操作，自带二维码和「一键添加到播客 App」

---

## 1. 为什么能"续播"

播客客户端本身就会记住 **每一集的播放进度**。所以服务端要做的是：

* **整库订阅**（`/feed/library.xml`）：一个播客。默认每本书 = 一「季」，
  书里的每一集 = 一个单集，所以一个订阅里既能挑书也能挑集。
  把 `VOCALIS_LIBRARY_FEED` 设成 `books` 则变回"每本书只占一集"：一本书的多个
  mp3 会在服务端拼成一条连续音频流（HTTP Range 支持拖动进度）。
* **单本订阅**（`/feed/book/<id>.xml`）：一本书 = 一个播客，每章 = 一集，
  章节名和顺序都保留。

两种地址都提供，你想用哪种都行。

---

## 2. 快速开始

### 2.1 准备目录

在 NAS 上建两个目录：

```
/volume1/audiobooks    ← 你的有声书（只读挂载即可）
/volume1/vocalis/data ← 缓存、刮削结果、索引（需要可写）
```

有声书目录怎么放都行，程序会自动识别：

```
audiobooks/
├── 三体（刘慈欣 著）/            # 常规：一个文件夹一本
│   ├── 01 第1章.mp3
│   ├── 02 第2章.mp3
│   └── cover.jpg
├── 刘慈欣 - 球状闪电/            # "作者 - 书名" 也能认
│   ├── CD1 上/01.mp3            # 分卷 / CD 目录会自动合并成一本
│   └── CD2 下/01.mp3
├── 余华/                        # 作者文件夹下多本书
│   ├── 活着/...
│   └── 许三观卖血记/...
└── 呐喊 - 鲁迅.mp3               # 单个文件也是一本书
```

群晖的 `@eaDir`、QNAP 缩略图目录、macOS 的 `._xxx`、回收站等噪声目录会被自动忽略。

### 2.2 构建并启动

```bash
cd vocalis
cp .env.example .env      # 改里面的 LIBRARY_PATH 等设置
docker compose up -d --build
```

> 不想用 `.env`，就直接改 `docker-compose.yml` 里 `LIBRARY_PATH` 那一行。

打开 `http://NAS的IP:8080` 就能看到书架。

### 2.3 不用 compose 也可以

```bash
docker build -t vocalis:latest .
docker run -d --name vocalis --restart unless-stopped \
  -p 8080:8080 \
  -v /volume1/audiobooks:/audiobooks:ro \
  -v /volume1/vocalis/data:/data \
  -e VOCALIS_TITLE=我的有声书 \
  -e VOCALIS_TOKEN=改成你自己的随机字符串 \
  vocalis:latest
```

### 2.4 群晖 / 威联通（不同 CPU 架构）

NAS 一般是 `linux/amd64`（绝大多数）或 `linux/arm64`（部分 ARM 机型）。
`docker compose build` / `docker build` 会按 **构建机器** 的架构出镜像，
所以最省事的做法是直接在 NAS 上构建。

在自己的电脑上交叉构建：

```bash
# 只做 x86_64 的 NAS
docker buildx build --platform linux/amd64 -t vocalis:latest --load .

# 两种架构一起，推到自己的私有仓库（多架构镜像必须先 push 或 -o）
docker buildx build --platform linux/amd64,linux/arm64 \
  -t your-registry/vocalis:latest --push .
```

群晖也可以用「Container Manager → 项目 → 新增」，直接贴 `docker-compose.yml`。

---

## 3. 在 iPhone 播客 App 里添加

三种方式，任选其一：

**方式一：网页里点按钮（最省事）**
手机浏览器打开 `http://NAS的IP:8080`，点书架的「添加到播客 App」，
再点页面里的绿色按钮，会唤起播客 App 并订阅。

**方式二：手动添加**

1. 播客 App →「资料库」→ 右上角「⋯」→「通过 URL 添加节目」
2. 粘贴订阅地址，例如 `http://NAS的IP:8080/feed/library.xml`

**方式三：扫码**
在网页里点「添加到播客 App」会显示二维码，手机相机扫码即可打开订阅页。

> 设置了 token 之后地址形如
> `http://NAS的IP:8080/feed/library.xml?token=你的令牌`，
> 网页上显示和复制的地址已经带好 token，直接复制即可。

### 关于 HTTPS

同一个局域网内用 `http://` 通常没问题。如果要在外网访问，或者遇到
iOS 拒绝加载订阅源的情况，请套一层反向代理（Nginx Proxy Manager、
群晖反向代理、Caddy 等）提供 HTTPS，并把 `VOCALIS_BASE_URL` 设成
那个对外域名。设置之后，feed 里生成的音频地址才会是正确的 https 地址。

---

## 4. 刮削（自动补全元数据）

打开某本书的详情页：

* **自动刮削**：按书名 + 作者去 Apple Books 和 Google Books 搜索，取匹配度最高的一条，
  自动填入书名、作者、简介、年份、封面。
* **搜索候选**：列出前 8 条结果（带封面和匹配度），自己挑一条应用。
* **手动编辑**：完全手填。表单里勾选了「锁定这些字段」的字段，之后的刮削不会覆盖。

刮削结果保存在 `/data/overrides/<book-id>.json`，重新扫描书库也不会丢。

> 只设置了 `VOCALIS_USERNAME` / `VOCALIS_PASSWORD` 时，程序会自动生成一个访问
> 令牌写到 `/data/token.txt`，并把它加进订阅地址里——因为播客 App 没法方便地
> 输入 Basic Auth。想换令牌就删掉那个文件再重启。

> Google Books 没有 API Key 时每天有配额，配额用尽会自动跳过，只用 Apple Books。
> 可以用 `VOCALIS_GOOGLE_BOOKS_KEY` 提升配额。

### 完全手动指定元数据（可选）

在书目录里放一个 `.vocalis.json`（或 `book.json` / `metadata.json`），
优先级高于标签和文件名的推断：

```json
{
  "title": "三体",
  "author": "刘慈欣",
  "narrator": "李野默",
  "publisher": "重庆出版社",
  "year": "2008",
  "series": "地球往事三部曲",
  "seriesIndex": 1,
  "genres": ["科幻", "小说"],
  "description": "……",
  "cover": "cover.jpg"
}
```

---

## 5. 环境变量

所有配置都可以用环境变量或命令行参数给出，命令行优先。

| 环境变量 | 命令行 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `VOCALIS_LIBRARY_DIR` | `--library-dir` | `/audiobooks` | 有声书根目录 |
| `VOCALIS_DATA_DIR` | `--data-dir` | `/data` | 数据与缓存目录 |
| `VOCALIS_ADDR` | `--addr` | `:8080` | 监听地址 |
| `VOCALIS_BASE_URL` | `--base-url` | 空 | 对外地址，如 `https://books.example.com` |
| `VOCALIS_TOKEN` | `--token` | 空 | 订阅令牌，公网必设 |
| `VOCALIS_USERNAME` / `VOCALIS_PASSWORD` | `--username` / `--password` | 空 | Web 界面与 API 的登录账号 |
| `VOCALIS_TITLE` | `--title` | `我的有声书` | 播客标题 |
| `VOCALIS_LAYOUT` | `--layout` | `auto` | `auto` / `flat` / `nested` |
| `VOCALIS_SCAN_INTERVAL` | `--scan-interval` | `0` | 自动扫描间隔，如 `6h` |
| `VOCALIS_LANGUAGE` | `--language` | `zh-cn` | feed 语言 |
| `VOCALIS_COUNTRY` | `--country` | `cn` | 刮削优先区域 |
| `VOCALIS_GOOGLE_BOOKS_KEY` | `--google-books-key` | 空 | 可选，提升 Google Books 配额 |
| `VOCALIS_LOG_LEVEL` | `--log-level` | `info` | `debug` / `info` / `warn` / `error` |

`VOCALIS_LAYOUT` 说明：

* `auto`（默认）：按目录结构推断。有音频的文件夹就是一本；`CD1/CD2`、
  `上部/下部`、`正文`、`音频` 之类的子目录会合并进同一本；`作者/书名/` 会取到书名。
* `flat`：只认顶层目录，每个顶层目录 = 一本。
* `nested`：只要有音频文件的目录各算一本。

---

## 6. 接口

| 路径 | 说明 |
| --- | --- |
| `GET /` | 书架网页 |
| `GET /feed/library.xml` | 整库订阅（每本书一集） |
| `GET /feed/book/<id>.xml` | 单本订阅（每章一集） |
| `GET /stream/<book-id>` | 整本书合并后的音频流（支持 Range） |
| `GET /audio/<chapter-id>` | 单个音频文件（支持 Range） |
| `GET /cover/<book-id>` | 封面图（没有封面时自动生成一张） |
| `GET /chapters/<book-id>.json` | Podcasting 2.0 章节标记 |
| `GET /subscribe?u=<feed>` | 订阅引导页（含二维码、一键唤起播客 App） |
| `GET /api/stats` / `GET /api/books` | 书架信息（JSON） |
| `POST /api/rescan` | 重新扫描 |
| `GET /api/search?q=&author=` | 刮削候选（JSON） |
| `GET /healthz` | 健康检查（不需要鉴权） |

---

## 7. 常见问题

**Q：添加订阅后书没有立刻出现？**
播客客户端刷新 feed 有延迟（通常几分钟到一小时）。可以在节目页面下拉刷新，
或者删掉订阅再加一次。

**Q：进度会丢吗？**
每一集的进度由播客 App 按 `guid` 记录。章节的 `guid` 是根据文件相对路径算出来的，
同一本书里增删文件、重新扫描都不会改变已有章节的 `guid`，所以进度不会丢。
但如果你把音频文件本身换掉了，App 记的进度是旧的，需要重新开始。

**Q：为什么整库订阅里某本书是好几集？**
只有全是 MP3 的书才会被拼成一条流（MP3 帧可以直接拼接）。如果这本书是
`m4a` / `flac` / `wav` 等格式，为了避免音频损坏，会退化成"每章一集"。
你也可以直接订阅这本书的单本地址。

**Q：磁盘占用大吗？**
索引和刮削结果只有几百 KB；封面缓存在 `/data/covers`。
音频是边播边读，不会复制一份。

**Q：能听 m4b（自带章节的有声书）吗？**
能播。不过 m4b 内部的章节不会被解析成播客章节，整本作为一个音频。

**Q：想改书架标题或端口？**
改环境变量后 `docker compose up -d` 重启即可。

---

## 8. 本地开发

```bash
make test          # 单元测试（目录识别、时长解析、Range、鉴权等）
make run           # 用 ./testlib 作为书库，监听 127.0.0.1:8080
make build         # 产出 ./vocalis 二进制
```

代码结构：

```
cmd/vocalis          程序入口
internal/config       配置解析（环境变量 + 参数）
internal/library      扫描目录、识别书名/作者、解析标签与封面
internal/media        ID3/M4A/FLAC/WAV 标签与时长解析（不依赖外部程序）
internal/scrape       Apple Books / Google Books 刮削
internal/store        内存索引 + JSON 持久化 + 用户覆盖
internal/feed         RSS / Podcasting 2.0 feed 生成
internal/server       HTTP 服务：feed、音频 Range、封面、Web UI、API
internal/server/web   内嵌的网页模板与静态资源
```

外部依赖只有两个：`github.com/dhowden/tag`（读音频标签）和
`github.com/skip2/go-qrcode`（生成二维码）。
