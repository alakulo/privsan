# 操作与恢复手册

## 安装与升级

从受信任发布渠道取得对应平台可执行文件及校验和，核对 SHA256 后放到 PATH。项目目前没有指定公网分发渠道。Windows 使用 privsan.exe；Linux/macOS 需赋予执行权限。运行不需要 Go；源码构建使用 go.mod 指定的已修补工具链，首次下载依赖需要网络。升级前运行 version 并阅读接口版本变化，避免把原型 schema 1 接入直接用于 v2。

## 三条主要工作流

只读检查：

~~~powershell
.\privsan.exe scan --json --fail-on-findings .\documents
.\privsan.exe tui --dry-run .\documents
~~~

导出新目录：

~~~powershell
.\privsan.exe redact --output D:\sanitized\batch-001 D:\documents
~~~

导出目标必须不存在且不在输入根内；该限制也适用于单文件的父目录根。导出包含扫描范围内的全部受支持文件，不复制被排除/不支持的文件。仅退出码 0 且目标内有 .privsan-export.json 完成清单时使用结果。中断可能留下未完成目标，重试应使用新的目标目录。程序不会自动删除用户目录。

原地修改及恢复：

~~~powershell
.\privsan.exe redact --in-place --backup-dir D:\private-backups D:\documents
.\privsan.exe restore --from D:\private-backups\run-实际运行目录 --root D:\documents
~~~

处理前停止会改写来源的程序。备份路径由操作结果 run_dir 给出。单文件输入的 restore --root 指向该文件的父目录。备份中 .bin 是原始敏感字节；manifest.json 包含目标相对路径及恢复用哈希，不可当作公开审计报告。不要修改文件名或清单，不要把备份提供给 Agent。恢复会先验证全部备份和目标，再逐文件执行；已经恢复的文件不会再次改写。第三方修改会导致 E_CONFLICT，不提供默认强制覆盖。

备份保留和安全擦除由部署者负责。Windows 父目录必须配置只对当前用户/授权人员可读的 ACL；POSIX 默认新建目录 0700、文件 0600。软件不自动配置企业 ACL，也不承诺所有文件元数据都能保留。

## TUI

- 默认异步扫描启动路径；o 输入新路径，r 重扫。
- ↑↓、j/k、PageUp/PageDown、Home/End 导航；空格选择命中。
- / 输入文件路径片段或规则 ID；a 切换当前筛选中的全部项目；Esc 清除筛选。
- 下方预览显示全部识别结果已脱敏的行，即使某项被取消选择。预览不是将被部分保留写入的原文视图。
- d 输入新导出目录；w 打开确认，y 执行。若通过命令行设置 --in-place --backup-dir，则确认页显示备份目录。
- e 查看错误及操作目录；q 退出。扫描/写入期间 q/Esc/Ctrl+C 请求取消，等待当前单文件操作结束。
- 操作结束后禁止再次改选并复用旧快照；r 重扫后才能开始下一次操作。
- 最小终端 48 列 × 15 行。普通输出和 TUI 对文件名及预览文本转义，避免文件里的控制字符控制终端。

## Agent 接入

~~~powershell
.\privsan.exe scan --json --content --hide-paths D:\documents
~~~

可信本地程序必须先等待命令结束，检查退出码和 complete=true，然后只向模型提供 files[].content。扫描失败或取消时正文被整体隐藏。默认 --json 不带正文，适合本地审计计数。JSONL 最后一条必须是 summary；缺失尾记录视为失败。标准输入可用 scan --stdin --json --content 或 redact --stdin --stdout。

PowerShell 5.1 管道可能转换输入编码/换行；需要逐字节保留时优先传文件路径。直接 stdout 重定向不是文件事务，管道断开时消费者只能得到部分输出并收到非零退出码；可靠落盘请用 --output。

## 范围与规则

配置必须显式 --config。config init 输出默认策略，config validate 校验，不会覆盖现有配置。配置与 ignore 文件都是可信本地策略输入，第三方配置可能关闭关键检测。

默认识别常见 ASCII 邮箱；在日志中将 = 视为字段分隔符。带引号的邮箱、Unicode 邮箱和包含 = 的少见本地部分不承诺完整识别。大陆手机允许 3-4-4 空格/短横线分组及国家码；国际手机、扩展分机格式不在 v1 范围。身份证 strict 模式不验证完整行政区划库或真实性，candidate 模式用于减少因校验失败导致的遗漏。

重叠候选会合并为覆盖整个交集连通区的命中，掩码类型由最前、最长、优先级最高的候选确定；这可能把本来可单独选择的项目合为一项，优先保证不留下重叠尾部。规则使用 Go RE2，不支持回溯/反向引用/lookaround；replacement 是字面量，不展开 $1。

## 故障处理

| code | 处理 |
|---|---|
| E_CONFIG | 检查 schema version、拼写、重复键、正则与限制；先运行 config validate |
| E_INPUT / E_READ | 检查路径、权限、符号链接、是否被其他程序更改 |
| E_ENCODING | 转换为 UTF-8 并人工确认；程序不自动猜测编码 |
| E_LIMIT | 缩小目录或适度提高已公布上限；不能忽略后当作完整扫描 |
| E_CHANGED | 重新扫描；若已有部分提交，先查看备份再决定恢复 |
| E_CSV | 收窄跨 CSV 结构字符的规则；不要盲目排除文件来隐藏错误 |
| E_WRITE / E_BACKUP | 处理磁盘、权限或占用问题；保留运行目录并执行恢复 |
| E_CONFLICT | 当前文件有新改动；手动另存并比对，程序不会覆盖 |
| E_CANCELLED | 本批未完整完成；导出不消费，原地修改按恢复流程检查 |

提出问题时提供 version、退出码、问题 code、脱敏后的配置与可复现合成样本；不要发送真实原文、密钥或备份。
