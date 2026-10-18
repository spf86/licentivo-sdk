# Licentivo 客户端 SDK

[English](README.md) · [首次接入](docs/quickstart.zh-CN.md) · [官网文档](https://www.licentivo.com/zh/docs.html#sdk)

提供 Go、Java、C、C++、C#、Python、Node.js、Rust、Ruby 九种客户端和演示。SDK 处理激活、设备身份、验签、缓存、凭证保存、心跳以及功能计次。客户软件只需提供授权码和业务函数。

## 功能与工作方式

| 功能 | SDK 自动处理 | 应用需要提供 |
| --- | --- | --- |
| 激活与重启恢复 | 激活授权、保存本机设备凭证，下次启动自动恢复 | 激活输入框与产品配置 product.json |
| 设备绑定与换机 | 生成设备身份，支持主动解绑和换机临时码 | 用户主动换机时的操作入口 |
| 有效期与功能权限 | 检查授权和功能权益，允许后才执行回调 | 需要保护的导出、保存或计算函数 |
| 签名验证 | 用产品固定公钥验证签名，拒绝篡改和上下文不匹配的授权 | 正确的产品公钥配置 |
| 心跳与离线使用 | 按策略发送心跳，在策略允许范围内使用已验签的离线权益 | 应用运行期间保留 client，在控制台设置策略 |
| 浮动席位 | 为运行实例申请席位，关闭时尝试归还 | 应用退出时关闭 client |
| 功能计次 | 预留次数、执行回调、成功确认或失败取消，并保存待结算记录 | 稳定的任务编号及业务结果记录 |
| 离线文件激活 | 创建离线请求并验证导入的激活响应 | 文件导入与导出界面 |
| 状态与错误诊断 | 提供本地授权状态、功能状态及联网请求的结构化错误 | 用户提示和支持所需的 request ID |

计次业务需要联网。业务已成功而确认失败时，重试只补确认，不再次执行业务；业务执行中进程退出时，应用仍要根据已保存的结果判断实际是否成功。SDK 不代替应用判断外部业务结果。

## 选择安装方式

[官网安装文档](https://www.licentivo.com/zh/docs.html#sdk) 按语言展示已验证的包仓库版本和安装命令。对应平台版本可用时，使用 Go modules、pip/PyPI、npm、NuGet、Maven/Gradle、Cargo/crates.io 或 RubyGems；C/C++ 使用版本 CMake 包。下面的官网安装器覆盖全部九种语言，也支持使用已下载的包离线安装。

**0.10.0 已发布到公共平台**，在自己的应用项目中使用：

| 语言 | 安装命令或构建依赖 |
| --- | --- |
| Go | `go get github.com/spf86/licentivo-sdk@v0.10.0` |
| Python | `python -m pip install "licentivo-sdk==0.10.0"` |
| Node.js | `npm install @licentivo/sdk@0.10.0` |
| C# | `dotnet add package Licentivo.SDK --version 0.10.0` |
| Java | Maven 依赖 `com.licentivo:licentivo-sdk:0.10.0`；[Maven / Gradle 配置](sdk/java/README.md) |
| Rust | `cargo add licentivo@=0.10.0` |
| Ruby | `gem install licentivo-sdk --version 0.10.0`；Gemfile 使用 `gem 'licentivo-sdk', '= 0.10.0'` |
| C / C++ | [下载匹配编译器的包](https://github.com/spf86/licentivo-sdk/releases/tag/v0.10.0)，设置 `CMAKE_PREFIX_PATH`，再 `find_package(Licentivo 0.10 REQUIRED)` |

安装 SDK 无需管理 API Key。实际激活应用时，使用自己的产品配置与客户授权码。各语言版本要求、原生依赖和离线安装见[安装说明](docs/install.md)。

## 统一安装

九种语言共用 **0.10.0** 版本。官网按语言展示已验证的公共包仓库安装方式，同时提供安装包、发布清单和 SHA-256 校验文件。

在自己的项目目录运行。安装前可以查看下载的脚本；安装器在下载后核对校验值，失败时停止。Go 项目先有 go.mod，Node 项目先有 package.json，Rust 项目先有 Cargo.toml，Python 建议先激活虚拟环境。

Windows PowerShell：

```powershell
Invoke-WebRequest https://www.licentivo.com/assets/sdk/releases/v0.10.0/install.ps1 -OutFile install-licentivo.ps1
./install-licentivo.ps1 -Language python -Version 0.10.0
```

Linux / macOS：

```bash
curl --fail --location --proto '=https' --proto-redir '=https' --tlsv1.2 -o install-licentivo.sh \
  https://www.licentivo.com/assets/sdk/releases/v0.10.0/install.sh
bash install-licentivo.sh python --version 0.10.0
```

将 python 换成表格里的语言参数：

| 语言 | 参数 | 安装器完成的工作 |
| --- | --- | --- |
| Go | go | 下载独立模块，自动添加版本依赖和本地 replace，无需 GitHub 登录 |
| Java | java | 下载 JAR，在 Gradle dependencies 或 classpath 中引用 |
| C | c | 解压匹配平台的 CMake 包，链接 Licentivo::C |
| C++ | cpp | 解压匹配平台的 CMake 包，链接 Licentivo::CPP |
| C# | csharp | 下载 NuGet 包，保留原有项目 NuGet 配置，添加版本依赖并恢复 |
| Python | python | 用 pip 安装 wheel 和声明的依赖 |
| Node.js | javascript | 用 npm 安装 tarball，导入 @licentivo/sdk |
| Rust | rust | 自动用 cargo add 写入本地路径依赖，Cargo 构建授权核心 |
| Ruby | ruby | 优先安装匹配平台的预编译 gem，输出 GEM_PATH 命令，缺少匹配包时构建源码 gem |

多个 C# 项目时传 `-Project MyApp.csproj` / `--project MyApp.csproj`。Rust 也可传 Cargo.toml 路径。离线安装传 `-PackageDirectory release-files` / `--package-dir release-files`，目录需要安装包和 SHA256SUMS。仅下载传 `-DownloadOnly` / `--download-only`。

安装文件位于 `.licentivo-sdk/语言/v0.10.0/`。Go、Rust、本地 NuGet 源和 C/C++ 构建包需要保留此目录。Ruby 使用安装器输出的命令配置当前终端，不更改全局 gem 设置。Java/C/C++ 的构建引用示例、依赖和全部选项见[安装说明](docs/install.md)。

## 约 15 分钟接入自己的应用

1. 在控制台“开发与接入 → 快速接入”下载已配置的示例，取得 product.json 和私有的 sdk-demo.json。
2. 解压后运行 `./start.ps1 -CheckOnly`、`./start.ps1 -TestLicense`；Linux 使用 `bash start.sh --check-only`、`bash start.sh --test-license`。
3. 看到 `License OK: export is available`、`Export completed: licensed-report.txt`。去掉测试参数再次运行，确认自动恢复本机凭证。
4. 用官网安装器把 SDK 加入自己的项目。只将 product.json 加入软件资源，客户授权码由激活窗口取得。
5. 启动时 OpenWithLicense，普通功能将业务函数交给 RunFeature，计次功能交给 RunMeteredFeature，退出时关闭 client。

计次方法自动预留、确认、取消并保存待确认任务。应用保存稳定任务编号和业务结果，业务中途崩溃时，核对实际结果后再恢复结算。完整九语言代码和权限拒绝测试见[首次接入步骤](docs/quickstart.zh-CN.md)与[运行指南](sdk/README.md)。

product.json 只含产品编号、地址、公钥等可分发设置。客户授权码、设备凭证、缓存和 sdk-demo.json 都不能公开；厂商管理 API Key 仅放在厂商后端。

## 发布与验收

构建脚本检查九种语言的共同 VERSION，生成源码包、原生安装包、manifest.json 和 SHA256SUMS。Linux 完整构建与 Windows 原生构建合并后，再用统一安装器在空白项目中验证。`scripts/publish-website.py` 检查完整性并生成官网发布目录，同版本内容不可覆盖。流程见[维护者发布指南](docs/releasing.md)。

Windows x64、Linux x64 有实际运行验收。C/C++ 按编译器 ABI 分包；Ruby 平台 gem 无需安装 C 编译器。Rust Windows 使用 GNU/SEH MinGW 与匹配的 libcurl。macOS、ARM64 仍待实机验收，当前没有 Android/iOS 专用 SDK。

采用 [MIT](LICENSE)；第三方组件保留各自许可，见 [NOTICE](NOTICE)。

## 许可证有效期

有效天数在单张或批量签发许可证时填写，不在授权策略中设置。固定期限和试用许可证填写 `duration_days`（1–36500 天），永久授权不填。从首次成功激活开始计时；应用策略保留原期限、到期日和续期权益。现有 SDK 0.10.0 按签名中的到期时间校验，无需更改代码或配置。

[控制台与管理 API 说明](https://www.licentivo.com/zh/docs.html#management)
