# Atelier

个人综合桌面应用，用 [MyGo](https://github.com/egoist/mygo) 原生 UI 编写，界面组件来自自己维护的组件库 [mallow](https://github.com/pandyzhou/mallow)，有新拟态和自然有机两种风格，在设置里切换：界面全由 Go 代码描述并在 GPU 上绘制，不依赖浏览器内核，打包后是一个只有几 MB 的可执行文件。

目前的功能：

- **首页**：给 AI 助手写一句话。可以问某天有什么课、什么任务，也可以让它添加、修改和删除任务与课程，设置每天的节数和各节的时间；还可以问它 GitHub 这周有什么热门项目、某个仓库是做什么的，它会去读热门列表和仓库的 README。可以带上图片、PDF、Word、Excel、PPT（.docx、.xlsx、.pptx）和文本文件：点回形针选择，或者拖进窗口、直接粘贴截图（看图需要模型支持；文档只读其中的文字，扫描件读不了）。左上角可以换模型、调思考强度（默认、低、中、高，按模型分别记住），模型给出的思考过程折叠显示在回答上方，回答逐字输出、按 Markdown 显示，可以停止、复制、重新生成；对话保存在本地，右上角的按钮打开历史对话。模型需要支持工具调用。
- **待办**：按日期看的任务。
  - **视图**：今天（含已过期）、最近 7 天、按周（可翻到上一周和下一周）、全部。勾掉的任务当天留在“今天完成的”里，之后在“按周”里它到期的那一天下面能看到。
  - **任务**：在列表里点右键选“新任务”，在弹出的对话框里填好再添加；点任务，它在原地展开成一张卡片，可以改标题、备注、日期（日历选择）和重复；右键任务可以改到今天或明天、设为每周重复、标为完成或删除。
  - **重复**：每天、每周或每月。勾掉一次，会留下一条已完成记录，任务自动跳到下一次。没勾也不会卡在过去：到了新的一天、新的一周（周一起算）或新的一个月，它自动换成这一期的，之前没做的不保留。“最近 7 天”和“按周”里，以后各期会提前显示在它们的日子下面，颜色淡一些，不带勾选框，到期前不能勾；点它可以打开任务修改。
  - 以前“每周清单”的数据会在第一次启动时自动转成任务（原来的 `weekly.json` 保留不动）。
- **课表**：一周的课程，每周相同，每门课一种颜色。平时只查看；点右上角的铅笔才能修改：点空格添加，按住拖过几节添加连上几节的课，点课程编辑，每天几节、每节的起止时间在“节次时间”里设。填了每节的开始时间后，上课前会弹出系统通知提醒（默认提前 20 分钟，可在设置的“通用”里关掉或改时间；应用要开着，关到托盘里也算）。
- **月历**：同一批任务按月看。每天一格，任务显示在到期那天，重复的任务以后各期用淡色显示在它们的日子里；点任务在对话框里编辑，右键某一天可以在那天新建任务。
- **热门**：GitHub 这一周的热门仓库，读自 github.com/trending。打开页面时自动读取，超过 6 小时会重新读，也可以手动刷新；点一个仓库打开它自己的页面，显示简介和 README（带图片），点“翻译成中文”让你选的模型写一段简介并把 README 译成中文，译文会保存下来。

## 开发

需要 Go 1.27.3 或更高版本。

同时修改 mallow 时，把它克隆到旁边的 `../mallow`，再执行 `go work init . ../mallow`：本地编译就会直接用 mallow 的源码（`go.work` 不提交）。改完 mallow 后先给它打新版本标签，再在这里 `go get github.com/pandyzhou/mallow@<新版本>`，GitHub 上的构建用的是 `go.mod` 里锁定的版本。

```sh
go tool mygo dev     # 开发模式运行，改代码后自动重启
go test ./...        # 不开窗口测试界面
go tool mygo build   # 打包
```

不开窗口把界面渲染成图片：

```sh
ATELIER_SCREENSHOT=shot.png go test -run Screenshot .
```

## 发布

GitHub Actions（`.github/workflows/windows.yml`）会打两个 Windows 包：安装包 `atelier-setup-<版本>-amd64.exe`，以及便携版 `atelier-<版本>-windows-amd64-portable.exe`（单文件，双击即可运行）。

- **推送到 `main` 就会构建并发布新版本**，版本号自动递增最后一位：`mygo.json` 的 `version` 决定主次版本，例如 `0.1.0` 之后依次发布 `v0.1.1`、`v0.1.2`……想升主次版本时把它改成 `0.2.0`，下次推送就从 `v0.2.0` 开始。
- Release 描述是上一个版本以来所有提交的描述（去掉署名行），所以提交描述要写清楚改了什么。
- 只想试打包：在 GitHub 的 Actions 页面手动运行 Windows 工作流，产物会保存在这次运行的 Artifacts 里，不会创建 Release。

数据以 JSON 保存在 `%AppData%\Atelier`（macOS 为 `~/Library/Application Support/Atelier`）。

## 结构

```
main.go                      外壳：侧边栏 + 当前功能页面，在 features() 里注册功能
internal/feature/            Feature 接口：ID、Title、View
internal/store/              每个功能一个 JSON 文件的存储
internal/features/home/      首页：AI 助手
internal/features/trending/  热门：GitHub 每周热门仓库
internal/features/todo/      待办
internal/features/timetable/ 课表
```

新增功能：在 `internal/features/` 下建一个包，实现 `feature.Feature`，再加到 `main.go` 的 `features()` 里。
