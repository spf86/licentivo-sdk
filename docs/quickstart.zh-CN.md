# 首次接入：让自己的导出功能受授权保护

目标：约 15 分钟完成一次激活、实际导出一个文件，再把授权入口放进自己的应用。前提是已经安装对应语言的开发环境。编译器安装、陌生项目改造和原生依赖配置可能需要额外时间。

SDK 负责组装请求、识别本机设备、验签、保存设备凭证和缓存、发送心跳。普通接入无需实现文档里的 HTTP JSON，也不必自行保存 activation_id。应用只提供客户的激活输入和实际业务函数。

现支持 Go、Java、C、C++、C#、Python、Node.js、Rust、Ruby 九种语言。

## 1. 取得自己产品的配置（约 2 分钟）

在控制台打开“开发与接入 → 快速接入”，填写软件名称，首次接入建议选择“绑定设备”，创建产品并选择语言，下载**已配置的源码包**。

默认示例有 `export` 功能。已有产品应确保策略和许可证允许 `export`，先不配置次数限制。解压后保留目录结构：

- `product.json`：产品 ID、服务地址和可信公钥，可随自己的应用分发。
- `sdk-demo.json`：含私有测试授权码，不分发给客户。
- `start.ps1` / `start.sh`、`SDK-LANGUAGE`、`sdk/`：检查和运行所选语言。Go 包还在根目录包含模块和客户端源码。

## 2. 检查环境并生成一个导出文件（约 3 分钟）

| 语言 | 需要的开发环境 |
| --- | --- |
| Rust | Rust 1.77+、C11 编译器、libcurl 开发包，Linux 还需 OpenSSL。Windows 使用 GNU/MinGW 目标和现代 SEH 编译器。 |
| Ruby | Ruby 3.1+、Fiddle、C11 编译器、libcurl 开发包，Linux 还需 OpenSSL；安装源码 gem 还需要 make。 |
| Go | Go 1.26+ |
| Python | Python 3.10+，venv 和 pip；脚本在当前目录创建虚拟环境 |
| Node.js | Node.js 20+；运行源码示例无需 npm 安装 |
| Java | JDK 17+，包括 javac |
| C# | .NET SDK 8+，依赖可从 NuGet 获取或已缓存 |
| C / C++ | C11 / C++17、CMake 3.18+、libcurl 开发文件；Linux 还需 OpenSSL 开发文件。Windows 编译器、库架构和 DLL 必须匹配 |

在解压根目录打开终端。Windows PowerShell：

```powershell
./start.ps1 -CheckOnly
./start.ps1 -TestLicense
```

Linux / macOS：

```bash
bash start.sh --check-only
bash start.sh --test-license
```

第一条命令检查文件和工具，不激活授权，也不验证网络证书。第二条构建所选语言并把私有测试码传给子进程，不修改 `product.json`。成功应退出 0、生成 `licensed-report.txt` 并输出：

```text
License OK: export is available
Export completed: licensed-report.txt
```

再运行一次，**不加测试参数**。SDK 自动恢复本机凭证；绑定设备不会重复占用名额。正式用户在应用的激活输入框填写自己的授权码，不需要操作环境变量。

若 Windows 策略阻止已下载的脚本，先检查源码，再仅对该文件运行 `Unblock-File ./start.ps1`。MinGW 未安装 CMake 时，可传 `-CurlRoot C:/path/to/curl` 使用直接编译方式。指定 Python 可用 `-PythonExe` 或 `--python`。SDK 保持 TLS 校验；浏览器接受证书警告不代表 SDK 也信任该证书。

## 3. 放进自己已有的项目（约 4 分钟）

正式集成在[官网安装文档](https://www.licentivo.com/zh/docs.html#sdk)按语言选择已发布的包管理器命令；统一安装器覆盖九种语言的官网包与离线安装，共用 0.10.0 版本，先校验 SHA-256，再安装项目依赖或取得构建包。Go 和 Rust 自动添加依赖，Java/C/C++ 提供对应构建引用。

使用已下载的接入包离线安装时，把下表 `SDK_ROOT` 换成解压目录的**绝对路径**，在已有项目中执行。

| 语言 | 本地依赖与引入 | 下载包中的可运行文件 |
| --- | --- | --- |
| Rust | Cargo.toml 的 `[dependencies]` 加 `licentivo = { path = "SDK_ROOT/sdk/rust" }` | `sdk/rust/examples/quickstart.rs` |
| Ruby | Gemfile 加 `gem "licentivo-sdk", path: "SDK_ROOT/sdk/ruby"`，运行 `ruby SDK_ROOT/sdk/ruby/build_native.rb` 后 `require "licentivo"` | `sdk/ruby/examples/quickstart.rb` |
| Go | `go mod edit -require=github.com/spf86/licentivo-sdk@v0.10.0`，再执行 `go mod edit -replace="github.com/spf86/licentivo-sdk=SDK_ROOT"`；引入 `licentivo "github.com/spf86/licentivo-sdk"` | `sdk/go/quickstart/main.go`；全语言源码导出中为 `examples/go/quickstart/main.go` |
| Python | 在应用虚拟环境中执行 `python -m pip install "SDK_ROOT/sdk/python"`；`from licentivo import Client` | `sdk/python/quickstart.py` |
| Node.js | `npm install "SDK_ROOT/sdk/javascript"`；应用中 `import {Client} from '@licentivo/sdk'` | `sdk/javascript/quickstart.mjs`，将源码中的相对引入改为包引入 |
| Java | 将 `SDK_ROOT/sdk/java/src/licentivo` 加入项目源码目录，或构建本地 JAR；`import licentivo.Client` | `sdk/java/src/licentivo/Quickstart.java`，按自己的项目调整包名和类名 |
| C# | `dotnet add MyApp.csproj reference "SDK_ROOT/sdk/csharp/Licentivo.csproj"`；`using Licentivo;` | `sdk/csharp/quickstart/Program.cs` |
| C / C++ | CMake 中先将 `LICENTIVO_BUILD_EXAMPLES`、`LICENTIVO_BUILD_TESTS` 设为 OFF，再 `add_subdirectory("SDK_ROOT/sdk" licentivo-build)`，应用链接 `Licentivo::C` 或 `Licentivo::CPP` | `sdk/c/quickstart.c` / `sdk/cpp/quickstart.cpp` |

仅将 `product.json` 加入安装资源，并用应用的资源路径定位它。不要依赖客户终端的当前目录。改动应用的三个位置：

1. **启动或激活窗口：** 用产品配置和客户授权码创建一个 client。后续启动传空字符串恢复本机凭证。界面程序在后台任务里完成首次网络激活，避免阻塞窗口。
2. **业务入口：** 把自己的导出函数传给 RunFeature。SDK 检查通过才调用函数；按钮、快捷键、菜单和命令行都使用同一个入口。保留 client，不在每次导出时重新激活。
3. **退出：** 关闭 client 停止心跳。浮动授权归还席位，绑定设备登记保留。仅客户主动换机时调用 Deactivate。

| 语言 | 普通功能的业务调用 |
| --- | --- |
| Rust | `client.run_feature("export", || export_report())?`，业务函数返回 `Result<T, licentivo::Error>` |
| Ruby | `client.run_feature("export") { export_report }`，业务失败抛出异常 |
| Go | `client.RunFeature("export", exportReport)`，函数返回 error |
| Java | `client.runFeature("export", this::exportReport)`，失败抛出异常 |
| C# | `await client.RunFeature("export", ExportReport)`，函数返回 Task |
| Python | `client.run_feature('export', export_report)`，失败抛出异常 |
| Node.js | `await client.runFeature('export', exportReport)`，支持异步函数 |
| C++ | `client.RunFeature("export", exportReport)`，失败抛出异常 |
| C | `ln_run_feature(client, "export", export_report, context)`，回调成功返回 1，失败返回 0 |

## 4. 验证权限拒绝（约 3 分钟）

确保策略不含 `licentivo_denied_probe`，输出路径尚不存在。Windows：

```powershell
./start.ps1 -Feature licentivo_denied_probe -Output denied-report.txt
```

Linux / macOS：

```bash
bash start.sh --feature licentivo_denied_probe --output denied-report.txt
```

应返回非零退出码，**不生成 denied-report.txt**。随后在自己的应用中检查同一业务的所有入口。Status / FeatureStatus 可用于显示授权状态，不发送网络请求；功能次数的余额仍须联网请求。

## 5. 需要计次时，继续使用 SDK

先在策略配置 `export` 次数规则。将导出函数交给对应方法：

| 语言 | 计次调用 |
| --- | --- |
| Rust | `client.run_metered_feature("export", 1, &job_id, || export_report())?` |
| Ruby | `client.run_metered_feature("export", 1, job_id) { export_report }` |
| Go | `client.RunMeteredFeature(ctx, "export", 1, jobID, exportReport)` |
| Java | `client.runMeteredFeature("export", 1, jobID, this::exportReport)` |
| C# | `await client.RunMeteredFeature("export", 1, jobID, ExportReport)` |
| Python | `client.run_metered_feature('export', 1, job_id, export_report)` |
| Node.js | `await client.runMeteredFeature('export', 1, jobID, exportReport)` |
| C++ | `client.RunMeteredFeature("export", 1, jobID, exportReport)` |
| C | `ln_run_metered_feature(client, "export", 1, job_id, export_report, context)` |

SDK 自动预留、执行回调、成功后确认、失败时取消，并在私有目录记录任务。新业务使用新编号；同一业务重试保留原编号、功能和数量。确认网络失败后，再调用同一任务只补确认，不重复业务；取消的任务重新执行需要新编号。

示例脚本可加 `-Metered -OperationID export-job-001`（Windows），或 `--metered --operation-id export-job-001`。计次必须联网；普通 RunFeature 不会扣次数。

若程序在业务执行中途崩溃，SDK 无法知道文件或外部操作是否已完成，因此不自动重做。核对自己的持久业务结果后调用 ResolveMeteredFeature（相应语言名称见源码）确认或取消，该方法不执行业务。任务过期后保留结果供核对，不盲目重复。一个 client 同时执行一个计次工作流；不同进程应使用不同任务编号。保留任务记录直到对应编号不再可能被重试。

## 遇到问题先看哪里

| 现象 | 检查 |
| --- | --- |
| 找不到 product.json | 是否在解压根目录；自己的应用是否使用资源绝对路径 |
| 缺少开发工具 | 先运行 CheckOnly；C/C++ 的编译器与 curl 库必须匹配 |
| TLS 失败 | 运行环境是否信任服务器证书，证书域名是否匹配；不要关闭校验 |
| FEATURE_NOT_ENTITLED | 策略和许可证是否都有同名功能；未通过时业务不应执行 |
| 已激活后仍要输入 | 是否使用相同用户、产品、地址和缓存路径；默认缓存位于用户私有目录 |
| 次数不足 | 计次需要联网；查看服务器功能额度，不把本地 CheckFeature 当成次数余额 |
| 确认待完成或结果未知 | 保留任务编号和业务结果，按上面的确认/恢复步骤处理 |

HTTP 协议、完整演示、离线文件、换机、浮动席位和维护期留在进阶文档。macOS 实机及 ARM64 验收尚未完成，Android/iOS 专用适配器暂未提供。

Rust / Ruby 的 Windows 源码演示需要 `-CurlRoot C:/path/to/curl`，或设置 `LICENTIVO_CURL_ROOT`。Rust 使用 GNU 目标和匹配的现代 SEH MinGW 编译器。两个 SDK 都含控制台应用演示：状态、普通导出、计次导出、联网刷新和主动换机。Rust 计次返回 `Execution::AlreadyCompleted`、Ruby 返回 `result.replay?` 时，本次跳过业务，应用应从自己的任务记录中读取已有结果。
