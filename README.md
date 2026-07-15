# cloudstream

## Docker 管理员密码重置

随机生成新密码并在终端中显示一次：

```bash
docker exec cloudstream ./cloudstream admin password
```

手动指定新密码：

```bash
docker exec cloudstream ./cloudstream admin password 'NEW_PASSWORD'
```

命令只修改现有数据库中的唯一管理员密码，不会删除云账户、任务、通知或媒体服务器配置。重置成功后，旧会话的后续请求会失效；已建立的日志或任务长连接最迟约 10 秒断开。

手动指定的密码会短暂出现在 Shell 历史和进程参数中。在共享服务器上优先使用随机模式；如需手动指定，使用后应清理相应历史记录。
