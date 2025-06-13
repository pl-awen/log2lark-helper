
# 日志监控助手

用于实时监控多个日志文件或目录，解析特定格式的日志行，匹配指定级别的日志（如 `ERROR`），并通过 Lark Webhook 发送告警消息。程序支持自定义日志格式、消息模板、起始监控行数，并提供偏移量持久化和 Webhook 签名验证功能。

## 功能特性

- **多日志文件监控**：支持同时监控多个日志文件，通过 `--log-files` 指定。
- **自定义日志解析**：通过 `--log-regex` 定义正则表达式，结合 `--time-index`、`--level-index`、`--content-index` 提取时间戳、日志级别和 JSON 内容。
- **日志级别匹配**：通过 `--match` 正则表达式过滤特定级别（如 `ERROR|CRITICAL`）。
- **自定义消息格式**：通过 `--message-format` 使用占位符（如 `{file}`、`{msg}`）定义告警消息模板。
- **灵活的起始行数**：通过 `--start-line` 为每个日志文件指定起始监控行数，自动计算偏移量。
- **Webhook 签名验证**：支持 Lark Webhook 的密钥签名（`--webhook-secret`），签名每 3600 秒刷新。
- **偏移量持久化**：将每个文件的读取偏移量保存到 `--offset-file`（默认 `monitoring_offset.json`），支持断点续读。

## 使用方法

### 示例
```shell
# 默认日志格式支持 go-kratos 框架日志
go run ./cmd/log2lark-helper.go --webhook-url="https://open.larksuite.com/open-apis/bot/v2/hook/xxxxxx-xxxx-xxxx" --webhook-secret="xxxxx" --log-files="/Users/awen.liang/Awen/Dev/echo_maker_service/cmd/echo_maker_service/logs/kratos.log" --match="ERROR"

# NestJs 框架的 Winston 日志格式
go run ./cmd/log2lark-helper.go --json-part-content-index="#1" --level-field-index="level" --message-format='🚨 错误日志告警\file}\n时间: {timestamp}\n级别: {level}\n服务: {service}\n错误信息: {message}\n' --log-regex="(\{.*\})" --content-fields="#1,service,message,timestamp" --webhook-url="https://open.larksuite.com/open-apis/bot/v2/hook/xxx-xxx-xx" --webhook-secret="xxxxxxxxx" --watch-dirs="/Users/awen.liang/Awen/Dev/hkpc_project/logs/app" --match="error"

# 更多根据需要配置
# ...
```

### 命令行参数

| 参数                              | 描述                         | 默认值                                                                                                                               | 示例                                                                      |
|---------------------------------|----------------------------|-----------------------------------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------|
| `--log-files`                   | 逗号分隔的日志文件路径（与--watch-dirs其一必填） | 无                                                                                                                                 | `/var/log/app1.log,/var/log/app2.log`                                   |
| `--watch-dirs`                  | 逗号分隔的日志目录路径（与--log-files其一必填） | 无                                                                                                                                 | `/var/log/app1,/var/log/app2`                                           |
| `--watch-dir-file-suffix`       | 逗号分隔的目录下文件后缀               | .log                                                                                                                              | `.log,.logger`                                                          |
| `--webhook-url`                 | Lark Webhook URL（必填）       | 无                                                                                                                                 | `https://open.feishu.cn/open-apis/bot/v2/hook/xxx`                      |
| `--webhook-secret`              | Webhook 签名密钥               | 空                                                                                                                                 | `your-secret-key`                                                       |
| `--match`                       | 匹配日志级别的正则表达式               | `ERROR`                                                                                                                           | `ERROR\|WARN`                                                           |
| `--offset-file`                 | 偏移量存储文件                    | `.offset.json`                                                                                                                    | `/path/to/offset.json`                                                  |
| `--log-regex`                   | 解析日志行的正则表达式                | `^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3})\s+(\w+)\s+(\{.*\})$`                                                               | `^\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3})\]\s+(\w+)\s+(\{.*\})$` |
| `--json-part-content-index`     | JSON 内容捕获组索引               | #3                                                                                                                                | `3`                                                                     |
| `--level-field-index`           | LEVEL 内容捕获组索引              | #2                                                                                                                                | `3`                                                                     |
| `--content-fields`              | JSON 内容字段                  | #1,#2,#3,service.id,msg,caller,trace.id,span.id                                                                                   |
| `--message-format`              | 告警消息模板                     | `🚨 错误日志告警\n文件: {file}\n时间: {#1}\n级别: {level}\n服务: {service}\n错误信息: {msg}\n调用者: {caller}\nTraceID: {trace_id}\nSpanID: {span_id}` | `警报: {file} 在 {timestamp} 发生 {level} 错误: {msg}`                         |
| `--last-start-line`             | 逗号分隔的起始行数，与 `--log-files` 对应 | 空（从末尾开始）                                                                                                                          | `0`                                                                     |
| `--cache-time-second`           | 缓存时间                       | 1                                                                                                                                 | `3600`                                                                  |
| `--cache-content-index`         | 缓存内容捕获组索引                       | msg                                                                                                                               | `#1`                                                                    |


**--content-fields 消息模板占位符示例**：
- `{file}`：日志文件路径
- `{#1}`：时间戳
- `{level}`：日志级别
- `{service_id}`：服务 ID（`service.id`）
- `{msg}`：错误信息（`msg`）
- `{caller}`：调用者（`caller`）
- `{trace_id}`：追踪 ID（`trace.id`）
- `{span_id}`：Span ID（`span.id`）


### 配置 Lark Webhook

1. 打开 Lark 客户端，进入目标群聊。
2. 点击群设置 → 添加机器人 → 选择“自定义机器人”。
3. 命名机器人，启用“消息”权限，获取 Webhook URL。
4. 启用“签名校验”，获取密钥，分别用于 `--webhook-url` 和 `--webhook-secret`。


## 部署

### 1、使用 `supervisorctl` 部署为后台服务：

```bash
sudo vi /etc/supervisord.conf
```

```editorconfig
;log2lark-helper
[program:log2lark-helper]
command=/app/src/log2lark-helper/build/log2lark-helper --webhook-url="https://open.larksuite.com/open-apis/bot/v2/hook/xxx-xxx-xx" --webhook-secret="xxxxx" --log-files="/app/logs/dev.log" --match="ERROR"
stdout_logfile=/app/logs/log2lark-helper.log
```

```bash
sudo supervisorctl update
```


### 2、使用 `systemd` 部署为后台服务：

1. **创建服务文件**：
   ```bash
   sudo vi /etc/systemd/system/log2lark-helper.service
   ```

2. **添加配置**：
   ```ini
   [Unit]
   Description=日志监控服务
   After=network.target

   [Service]
   ExecStart=/xxxx/yyy/log2lark-helper --log-files="/var/log/app1.log,/var/log/app2.log" --webhook-url="https://open.feishu.cn/open-apis/bot/v2/hook/xxx" --webhook-secret="your-secret-key" --match="ERROR" --offset-file="/xxxx/yyy/log2lark_helper_offset.json"
   Restart=always
   User=nobody
   Group=nogroup

   [Install]
   WantedBy=multi-user.target
   ```

3. **启动服务**：
   ```bash
   sudo systemctl enable log2lark-helper
   sudo systemctl start log2lark-helper
   ```

4. **检查状态**：
   ```bash
   sudo systemctl status log2lark-helper
   ```