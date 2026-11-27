# mai comic

<img src="resources/icon.png" width="96" height="96" alt="mai comic 应用图标" />

[下载最新版](https://github.com/towmai155/mai-comic/releases/latest)

用 Go 和 MyGo Native UI 编写的 Windows 和 Android 漫画阅读器。导入图源后，默认搜索全部图源，结果展示封面、标题和来源。也可以在图源列表里选一个源单独搜索。开启「严格搜索」后，只显示标题完全匹配的结果。打开章节后向下滚动阅读，历史会记录章节和页码。

导入拷贝漫画图源后，可从独立的「男频榜」标签查看日榜、周榜、月榜和总榜。每张榜单都会在当前会话中记住自己的滚动位置。

在漫画的章节列表点「收藏漫画」，之后可从「收藏」重新打开。「收藏」和「阅读历史」以网格展示封面、标题和图源，点击历史卡片可继续阅读。手机长按卡片、桌面右键点击卡片，可以取消收藏或删除历史。收藏和历史保存在当前设备。

章节目录每行显示三章，可切换「正序」和「倒序」。应用通过标题里的章号判断先后，无法判断时以图源提供的顺序为正序。手机长按带专用适配器的章节可选择「在原站打开」。

手机左上角菜单、桌面侧栏和阅读页的「设置」都可以选择白天、夜晚或跟随系统，选择会保留到下次打开。

阅读页默认隐藏控件。点按漫画画面显示章节按钮、设置和底部进度条，再点按画面收起。拖动进度条可预览目标页码，松手后跳到该页。也可以点按进度条快速跳页。

## Windows 运行

在仓库根目录打开 PowerShell 并启动应用。Go 版本见 `go.mod`。

```powershell
go tool mygo dev
```

要体验内置的原创试读内容，在另一个 PowerShell 窗口运行本地图源服务。

```powershell
go run ./cmd/demo-source
```

在应用左侧粘贴 `http://127.0.0.1:8787/sources.json` 并点击「导入图源」。在 JSON 图源里搜索「远足」，在 HTML 图源里搜索「纸灯」。试读时保持本地服务运行。

点「刷新图源」会重新拉取已保存的配置并按图源 ID 更新。也可以在输入框粘贴一个配置链接，只刷新这一份。升级后如果还没有保存的链接，粘贴原配置地址刷新一次即可。

```powershell
go test ./...
go tool mygo build -skip-installer
```

Windows x64 的可执行文件位于 `build/windows-amd64`。

## Android

支持 Android 8.0（API 26）及以上。手机上用顶部标签切换历史、收藏和找漫画。左上角菜单打开图源侧栏，可导入、刷新和选择图源。应用内阅读时，继续向下滑过最后一页即可自动打开章节列表中的下一章。退出章节或把应用切到后台时会保存页码。

在 Windows 上打包时，将环境变量指向本机的 Android SDK 和 JDK，然后在仓库根目录运行命令。

```powershell
$env:ANDROID_HOME = 'C:\path\to\Android\Sdk'
$env:JAVA_HOME = 'C:\path\to\JDK'
go tool mygo build -platform android
```

MyGo v0.10.0 首次生成的 Gradle 工程缺少 Windows 启动脚本。如果构建提示找不到 `gradlew.bat`，从同版本的 Gradle 下载脚本到已生成的工程，再执行打包命令。

```powershell
Invoke-WebRequest 'https://raw.githubusercontent.com/gradle/gradle/v8.14.3/gradlew.bat' -OutFile '.mygo/android/gradlew.bat'
go tool mygo build -platform android
```

APK 和 AAB 位于 `build/android`。APK 可以直接安装到 Android 设备。使用本机的试读服务时，先运行上面的试读服务，再通过已连接设备的 ADB 转发端口。

```powershell
& "$env:ANDROID_HOME\platform-tools\adb.exe" reverse tcp:8787 tcp:8787
```

然后在手机上导入 `http://127.0.0.1:8787/sources.json`。公开 HTTPS 图源配置可以直接导入。

## 写自己的图源配置

也可以在「图源配置 URL」直接粘贴 Cimoc 的 `sourceBaseUrl.json` 地址。旧 GitCode 地址会自动使用同一项目的新下载地址。应用按 Cimoc 图源 ID 匹配内置适配器，其他条目在带有完整兼容规则时可以直接转换。漫画屋、咚漫漫画、动漫屋、漫画1234、漫漫漫画、腾讯动漫、读漫屋 org、大树漫画、漫客栈和漫画台（看漫画）支持在应用内读公开章节。使用专用解析器的图源可从章节列表选择「在原站打开」。WEBTOON 英文打开站点阅读页。

在图源页点「查看导入明细」，可以看上一次 Cimoc 导入中每条配置被跳过的原因。有有效首页地址的已识别站点会显示「打开原站」。能否访问取决于站点和当前网络。图片服务器、更新镜像等辅助地址没有原站入口。关闭应用后仍可查看上一次 Cimoc 导入的明细。

JMTT 可以通过已识别的分享短链导入。应用会在阅读器中加载章节图片并恢复分条图片的顺序。章节列表也提供「在原站打开」。

再漫画可以通过 `https://manhua.zaimanhua.com/` 单独导入，也可以从 Cimoc 配置中的 `HAOMAN8` 导入。再漫画支持站内搜索以及在应用内读公开章节。

可用同一个 URL 分发多个图源。配置的顶层字段是 `version: 1` 和 `sources` 数组。每个图源有 `id`、`name`、`baseUrl` 以及下面的四条规则。

| 规则 | 请求地址 | 提取字段 |
| --- | --- | --- |
| `search` | 相对于 `baseUrl` 的 `url`，包含 `{query}` | `title`、`url`，可选 `cover` |
| `details` | 搜索返回的漫画链接 | `title`，可选 `summary` 和 `cover` |
| `chapters` | 搜索返回的漫画链接 | `title`、`url` |
| `pages` | 章节链接 | `url` |

`details`、`chapters` 和 `pages` 可以指定相对于默认地址的 `url`。提取出来的相对链接以最终响应地址为基准。以同一个 `id` 重新导入会更新该源。

设置 `browserReader: true` 可用系统浏览器打开章节链接，此时可以省略 `pages`。应用内阅读进度适用于应用内打开的章节。

HTML 章节规则可指定 `next`，用 `选择器@href` 提取同站点的下一页链接。

- `format: "json"` 用点分路径提取字段。列表规则的 `items` 应对准数组。用 `"."` 表示当前值，适用于根数组或字符串数组。
- `format: "html"` 用 CSS 选择器。列表规则的 `items` 选择行，`fields` 在该行内选择元素。用 `a@href` 取子元素的属性，用 `@href` 取当前行的属性，用 `.` 取当前行的文本。

[试读服务的完整配置](internal/demosource/demo.go)包含两种格式的例子。当前支持 HTTP GET 返回的 JSON 或静态 HTML，以及可直接请求的图片地址。
