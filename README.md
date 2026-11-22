# Atelier

个人综合桌面应用，用 [MyGo](https://github.com/egoist/mygo) 原生 UI 编写：界面全由 Go 代码描述并在 GPU 上绘制，不依赖浏览器内核，打包后是一个只有几 MB 的可执行文件。

目前的功能：

- **每周清单**：一周七天并排，每天一列待办，可勾选、删除，能翻到上一周和下一周。

## 开发

需要 Go 1.27.3 或更高版本。

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
internal/theme/              新拟态（Neumorphism）主题与凸起/凹陷样式
internal/features/weekly/    每周清单
```

新增功能：在 `internal/features/` 下建一个包，实现 `feature.Feature`，再加到 `main.go` 的 `features()` 里。
