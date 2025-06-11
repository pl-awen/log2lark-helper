### 使用
```shell
go run ./cmd/log2lark-helper.go --webhook-url=https://open.larksuite.com/open-apis/bot/v2/hook/xxxxxx-xxxx-xxxx --webhook-secret=xxxxx --log-files=/Users/awen.liang/Awen/Dev/echo_maker_service/cmd/echo_maker_service/logs/kratos.log --match=ERROR
```

### 参数说明 
* --webhook-url: 机器人URL
* --webhook-secret: 密钥
* --log-files: 监听的日志文件，多个以 , 分割
* --match: 匹配的标识
* --offset-file: 记录监控日志的偏移值记录
* --log-regex: 匹配规则
* --time-index: 匹配规则中的时间索引
* --level-index: 匹配规则中的日志级别标识索引
* --content-index: 匹配规则中的日志内容索引
* --message-format: 日志格式