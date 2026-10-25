# 课堂通知

手机发一句话或一张照片，教室电脑弹出系统通知，并打开一个看得清的大窗口。给已经有公网 IP 和域名、但不想买 LED 屏的老师用。

不需要付费推送服务。教室电脑不用开端口，它自己连到你的服务器。

## 怎样算做好了

1. 服务器执行 `docker compose up -d --build`。
2. 教室电脑安装桌面程序。打开后立刻看到一串大号教室编号（首次自动生成，例如 `7K3M-P9WQ`）。服务器地址只填一次。
3. 手机浏览器打开同一个网址，把电脑上那串编号填进去，发送文字或照片。
4. 大约 2 秒内，教室电脑出现系统通知和大窗口。

## 架构

```
手机浏览器  --提交文字/照片-->  你的服务器  --推送-->  教室电脑托盘程序
                                  SQLite + 照片文件
```

- 手机只是一个网页，不用安装 App。
- 桌面程序是 Rust 写的托盘程序（Tauri，用系统自带网页组件，不是 Electron）。Windows 优先，macOS 可以用同一套代码编译。
- 电脑首次打开会自动生成一串难猜但好读的教室编号，大字显示在主界面和托盘里。手机填同一串即可，没有用户名和密码。
- 服务器如果设置了环境变量 `TOKEN`，手机和电脑还要再填同一个口令。
- 文字、照片都存在这台服务器上，之后还能按教室编号查看。

## 目录

| 路径 | 作用 |
| --- | --- |
| `server/` | 后端：接口、网页、SQLite |
| `desktop/` | 教室电脑托盘程序 |
| `deploy/Caddyfile` | HTTPS 反向代理 |
| `docker-compose.yml` | 一键启动 |

## 部署服务器

安装 Docker 后：

```bash
cp .env.example .env
```

公网使用时，在 `.env` 里设置 `TOKEN`（英文字母或数字，不要空格，最长 64 位）。只在教室局域网里用，可以留空，此时只靠教室编号配对。

```bash
docker compose up -d --build
```

浏览器打开 `http://服务器IP:8787`。健康检查：`http://服务器IP:8787/health`。

不用 Docker 时：

```bash
cd server
TOKEN=你的口令 DATA_DIR=./data PORT=8787 go run ./cmd/server
```

需要 Go 1.22 或更高版本。

### DNS 和端口

教室电脑只出站访问服务器，不要在教室电脑上做端口映射。

| 用途 | 放行 | 说明 |
| --- | --- | --- |
| 先用 HTTP | TCP `8787`（或 `.env` 里的 `PORT`） | `docker compose up` 默认这样 |
| 公网 HTTPS | TCP `80`、TCP `443` | 不要再对公网开放 8787 |

域名添加 A 记录（有 IPv6 再加 AAAA）指向这台机器，然后在 `.env` 里写：

```
DOMAIN=notify.example.com
HTTP_BIND=127.0.0.1
TOKEN=你的口令
```

`HTTP_BIND=127.0.0.1` 会把明文 8787 只绑在本机，避免和 HTTPS 同时对公网开放。然后：

```bash
docker compose --profile https up -d
```

Caddy 会向 Let's Encrypt 申请证书。手机和电脑都改用 `https://notify.example.com`。

不要给 `/api/v1/stream` 开 gzip，也不要让反向代理缓冲这个地址，否则通知会变慢。仓库里的 Caddy 配置没有开启压缩。

如果宽带是大内网、外面访问不到你的 IP，手机在校外会打不开网页。这种情况需要真正的公网 IP，或在路由器上做好端口转发。IP 变化时，改域名的 A 记录即可。

## 日常怎么用

1. 教室电脑打开「课堂通知」，窗口正中是教室编号。把这一串念给拿手机的人，或点「复制编号」。
2. 手机打开服务器网址（和电脑上填的是同一个）。输入电脑上那串编号，点「进入教室」。编号会记在这台手机上。
3. 写一句话或点「添加照片」，点「发送」。
4. 讲台电脑弹出系统通知和大窗口。需要回看时，手机点「历史消息」，或在电脑主窗口点一条记录。

照片请用 JPG、PNG、GIF 或 WebP，最大 12MB。iPhone 从相册选择时，一般会交给网页一份 JPG。

## 教室电脑：老师怎么装

教室电脑**不需要**安装 Rust 或 Node。老师从仓库 **Releases** 下载已经打好的 Windows 安装包即可：

1. 把 `课堂通知_*_x64-setup.exe`（或 `.msi`）拷到教室电脑。
2. 双击安装。若 Windows 提示「未知发布者」，选仍要运行。Windows 11 一般已有 WebView2；Win10 若提示缺少组件，按安装包引导下载即可。
3. 打开后立刻看到大号教室编号，例如 `7K3M-P9WQ`，以及「把这个编号发给手机」。
4. 第一次再填一次服务器地址（`https://你的域名` 或 `http://公网IP:8787`）。打包的人如果已经写入默认地址，这里是预填好的，点「保存并连接」即可。
5. 关掉窗口，程序留在托盘。托盘菜单第一行就是当前教室编号。下次开机可在托盘里打开「开机自启」。

退出请右键托盘图标，选「退出」。

### 谁来打 Windows 安装包

老师从仓库的 **Releases** 页下载 `课堂通知_*_x64-setup.exe` 即可，不必自己编译。发布人任选其一：

**方式 A：打标签自动发布（推荐）**

1. 把 `desktop/src-tauri/tauri.conf.json`（以及 `desktop/package.json`）里的 `version` 改成新版本，例如 `1.0.1`。
2. 若全校共用同一服务器，打开 `desktop/src-tauri/defaults.json`，把 `serverUrl` 写成学校的地址，例如 `"https://notify.example.com"`。
3. 提交后打标签并推送（标签名建议与版本一致）：

```bash
git tag v1.0.1
git push origin v1.0.1
```

4. Actions 工作流 `Windows desktop release` 会编译并把安装包挂到该版本的 Release。把下载链接发给老师。

**方式 B：网页上手动跑一次**

仓库 → Actions → Windows desktop release → Run workflow（默认建成草稿）→ 到 Releases 核对后点 Publish。

**方式 C：本机 Windows 打包**

必须在一台 **Windows** 电脑上。Linux / 云端编不出能双击的 exe。教室电脑本身不用参与编译。

1. 安装 [Rust](https://rustup.rs)、[Node.js](https://nodejs.org/)（LTS），以及 [WebView2](https://developer.microsoft.com/microsoft-edge/webview2/)（Win11 通常已有）。
2. 需要预填服务器时，同样改 `desktop/src-tauri/defaults.json`。
3. 在仓库里执行：

```bash
cd desktop
npm install
npm run build
```

4. 安装包在：
   - `desktop/src-tauri/target/release/bundle/nsis/课堂通知_1.0.0_x64-setup.exe`（推荐，双击安装）
   - `desktop/src-tauri/target/release/bundle/msi/`（可选）
5. 把 exe 发给老师。不要让老师在教室电脑上跑 `npm` 或 `cargo`。

开发调试（仅打包用的那台机器）：`cd desktop && npm run dev`。

推送到 `main` 且改动了 `desktop/` 时，工作流 `Windows desktop` 仍会编译并上传 **Artifact**（供自测），不会每次自动开新 Release。

### macOS

同样执行上面的 `npm run build`。程序在菜单栏。开机自启使用 LaunchAgent。

### Linux

编译前安装 WebKitGTK 4.1、GTK 3 和 Ayatana AppIndicator，例如 Ubuntu：

```bash
sudo apt install libwebkit2gtk-4.1-dev libgtk-3-dev libayatana-appindicator3-dev librsvg2-dev patchelf
```

然后同样 `npm install` 和 `npm run build`。

### 4K 屏幕

窗口使用逻辑像素，并跟随系统的每显示器 DPI。Windows 上由 WebView2 按 Per-Monitor V2 处理缩放。

如果 3840×2160 的屏幕在系统里仍是 100% 缩放，程序会按逻辑宽度相对 1920 再放大界面，最多约 2.4 倍，短消息用大号字，照片在窗口里完整显示。系统已经开了 150% 或 200% 时，不再额外放大，避免字被放两次。

## 历史记录

- 手机网页里的「历史消息」。
- 电脑托盘打开主窗口，点一条记录会用大窗口再看一次，照片会显示在窗口里。
- 接口：`GET /api/v1/messages?channel=301&limit=30`。下一页把上次返回的 `next_before` 原样放进 `before`。

数据在本机。Docker 卷名是 `notify-data`，里面有 `classroom.db` 和 `images/`。

```bash
docker compose exec server tar -C /data -cf - . > classroom-backup.tar
```

删掉这个卷会清空全部教室的消息和照片。

## curl 示例

没设口令时去掉 `X-Token`。

```bash
curl -sS -X POST http://127.0.0.1:8787/api/v1/messages \
  -H 'X-Channel: 301' \
  -H 'X-Token: 你的口令' \
  -F 'body=请翻开课本第12页'

curl -sS -X POST http://127.0.0.1:8787/api/v1/messages \
  -H 'X-Channel: 301' \
  -H 'X-Token: 你的口令' \
  -F 'body=黑板这道题' \
  -F 'image=@photo.jpg'

curl -sS 'http://127.0.0.1:8787/api/v1/messages?channel=301&limit=20' \
  -H 'X-Token: 你的口令'

curl -N 'http://127.0.0.1:8787/api/v1/stream?channel=301' \
  -H 'X-Token: 你的口令'
```

中文教室编号放在表单里，不要放进请求头：`-F 'channel=三年二班'`。

只发文字也可以用 JSON：

```bash
curl -sS -X POST http://127.0.0.1:8787/api/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'X-Channel: 301' \
  -H 'X-Token: 你的口令' \
  -d '{"body":"请安静"}'
```

## 安全说明

教室编号决定消息送到哪台电脑。`301` 这种短编号可以被猜到。`TOKEN` 是整台服务器的门禁：知道口令的人可以向任何教室发送。这是教室里的简单配对，不是每人一个账号。

公网部署请设置口令，并用上面的 HTTPS。照片按上传内容原样保存，可能包含相机信息。每间教室每分钟最多 30 条。

## 开发

```bash
make test
```

桌面程序：

```bash
cd desktop/src-tauri && cargo build
cd desktop && npm install && npm run dev
```

## 排错

- 手机打不开网页：检查域名是否指向这台机器、防火墙是否放行、是不是真的公网 IP。
- 电脑没有通知：看托盘状态是不是「已连接」。服务器地址不要写成教室电脑的 `127.0.0.1`，除非服务器就在这台电脑上。
- 开了 HTTPS 还能用 IP:8787 访问：检查 `.env` 的 `HTTP_BIND` 是不是仍为 `0.0.0.0`，应改成 `127.0.0.1`。
- 提示口令不正确：手机、电脑、服务器 `.env` 里的 `TOKEN` 必须一致。
- 通知来了但窗口被挡住：大窗口是置顶的，也可以在任务栏点「课堂通知」。
