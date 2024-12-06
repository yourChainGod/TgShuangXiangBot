# Telegram Bot

一个简单的Telegram 机器人，用于将用户消息转发给管理员，管理员回复消息转发给客户。

## 功能特点

- 消息转发：将用户消息转发给管理员
- 自动回复：支持自定义回复内容
- 教程功能：内置教程系统，帮助用户了解使用方法
- 数据持久化：使用 BoltDB 存储消息映射关系
- 日志系统：自动日志轮转，支持长期运行

## 重要说明

bot.go 中包含了自用的欢迎消息和教程说明，需要自行修改。

建议使用 polling 模式，webhook 模式需要配置 Webhook 回调地址和端口号，且需要配置 HTTPS 域名，麻烦！

### 配置文件

创建 `bot.yaml` 配置文件：

```yaml
account:
  token: "YOUR_BOT_TOKEN"    # Telegram Bot Token
  mode: "polling"           # 运行模式：polling 或 webhook
  endpoint: ""             # Webhook 模式下的回调地址
  port: 8443               # Webhook 模式下的端口号

admin:
  id: 123456789           # 管理员的 Telegram ID
```

## 运行

1. 直接编译后运行即可：
```bash
./tgbot
```

### 开机自启

添加到 crontab：
```bash
crontab -e
# 添加以下行
@reboot cd /path/to/bot && ./start.sh start
```

## 目录结构

```
.
├── bot.go          # 主程序文件
├── telegram.go     # Telegram API 相关代码
├── bot.yaml        # 配置文件
├── bot.log         # 日志文件
└── bot.db          # 数据库文件
```

## 常见问题

1. 数据库锁定问题
   - 解决方案：重启前确保正常停止机器人，或删除 `bot.db.lock` 文件

2. Webhook 模式配置
   - 需要有可用的 HTTPS 域名
   - 确保服务器防火墙允许指定端口访问

3. 日志文件过大
   - 系统会自动进行日志轮转
   - 可以手动删除旧的日志文件

## 安全建议

1. 不要将 bot token 直接硬编码在代码中
2. 定期检查日志文件是否有异常访问
3. 在生产环境使用 HTTPS
4. 定期备份数据库文件

## 许可证

MIT License
