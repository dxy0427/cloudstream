# cloudstream

## Docker 运行

先创建数据和 STRM 目录，并确保运行容器的用户可以写入：

```bash
mkdir -p /home/cloudstream/data /mnt/strm
chown -R "$(id -u):$(id -g)" /home/cloudstream/data /mnt/strm
```

启动容器：

```bash
docker run -d \
  -p 8091:8091 \
  -p 12398:12398 \
  -v /home/cloudstream/data:/app/data \
  -v /mnt/strm:/app/strm \
  --user "$(id -u):$(id -g)" \
  --name cloudstream \
  --restart unless-stopped \
  ghcr.io/dxy0427/cloudstream:latest
```

`/app/data` 保存数据库、JWT/流签名密钥和日志，必须持久化；丢失该目录会同时丢失配置，并导致已有签名 STRM 失效。任务的本地路径应使用容器内路径，例如 `/app/strm`。容器健康状态通过 `http://127.0.0.1:12398/healthz` 检查。

首次启动的用户名为 `admin`。未设置 `CLOUDSTREAM_ADMIN_PASSWORD` 时，随机密码只会在容器日志中显示一次，可通过 `docker logs cloudstream` 查看。修改云账户的 STRM 签名开关后，需以覆盖模式执行关联任务，重写已有 STRM 文件。

推送分支或 tag 不会自动构建镜像。在 GitHub Actions 页面打开“手动构建 YSTRM 镜像到 GHCR”，点击 Run workflow 后选择 `v2`，并指定镜像标签和目标平台；`v2` 分支中的工作流会先运行后端和前端测试，全部通过后再推送指定标签及提交 SHA 标签。

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
