# Hiraku

翻译版本：[English](README.md) | [日本語](README.ja.md)

Hiraku 是一个小型中继 agent，用来在远端机器上运行受限的调谐器命令模板，并通过 TCP 把输出流传回 Mirakurun。

这个仓库提供两个命令：

- `hirakud`：运行在调谐器所在主机上，只通过 TCP 暴露配置中允许的 pipeline。
- `hiraku`：运行在 Mirakurun 主机上，向 `hirakud` 请求一个已配置的 mode，并把收到的流写到 stdout。

## 构建

```sh
go build ./cmd/hirakud
go build ./cmd/hiraku
```

把 `hirakud` 安装到调谐器主机，把 `hiraku` 安装到 Mirakurun 主机。

## Mirakurun

推荐使用 [makeding/Mirakurun](https://github.com/makeding/Mirakurun) 这个 fork。

这套远程调谐器配置需要让 `BS` / `CS` 和 `BS4K` 使用不同命令，所以 Mirakurun 侧需要支持 `commandBS4K`。没有 `commandBS4K` 的 Mirakurun 无法只针对 `BS4K` 调用单独的 `hiraku` mode。

如果希望在返回之前把 BS4K 的 MMTS 转成 MPEG-2 TS，请在调谐器主机侧的 pipeline 中加入 [makeding/hantto4k](https://github.com/makeding/hantto4k)。

把 `hiraku` 当作普通的 Mirakurun tuner command 使用。mode 名必须和 `hirakud` 配置里 `modes` 下的 key 一致。

```yaml
command: hiraku 192.168.1.102:40773 change-me BSCS1 <channel>
commandBS4K: hiraku 192.168.1.102:40773 change-me BS4K1 <channel>
```

`BSCS1` 会把收到的 channel 直接传给 `recdvb`。`BS4K1` 会把收到的 channel 直接传给 `recdvb4k --b61`。实际执行的命令和参数由调谐器主机上的 `hirakud` 配置决定。

如果要使用通过 `hantto4k` 转换的 mode，就把 `commandBS4K` 指向转换用 mode。

```yaml
commandBS4K: hiraku 192.168.1.102:40773 change-me BS4K1-M2TS <channel>
```

## 安全

请仅在可信内网中使用本项目。通信内容未经过加密处理，请勿暴露到公网。

## hirakud 配置

完整示例见 `examples/pt4k-config.json`。

```json
{
  "listen": "0.0.0.0:40773",
  "secret": "change-me",
  "disconnectCloseDelaySeconds": 2,
  "modes": {
    "BSCS1": {
      "record": [
        ["recdvb", "--dev", "0", "--strip", "--b25", "<channel>", "-", "-"]
      ]
    },
    "BS4K1": {
      "record": [
        ["recdvb4k", "--dev", "0", "--b61", "<channel>", "-", "-"]
      ]
    },
    "BS4K1-M2TS": {
      "record": [
        ["recdvb4k", "--dev", "0", "--b61", "<channel>", "-", "-"],
        ["hantto4k", "--frontend-descrambled", "-", "-"]
      ]
    }
  }
}
```

调谐请求只能指定 `mode` 和 `channel`。`mode` 是 `modes.*.record` 下本地命令模板的标识，`hirakud` 只会替换模板里的 `<channel>`。pipe 请求同样只能选择 `pipes` 中预配置的名称。客户端不能发送任意 shell 命令。

`allowIPv4CidrRanges` 会在请求认证前限制客户端来源 IPv4 地址。省略时默认使用类似 Mirakurun 的局域网 IPv4 范围：`10.0.0.0/8`、`127.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`。

`disconnectCloseDelaySeconds` 是客户端 TCP 连接断开后，停止本地录制命令前等待的秒数。省略时默认是 `0`，也就是断开后立即停止命令。

pipeline 是 argv 数组的数组。执行时不经过 shell，每一步的 stdout 会接到下一步的 stdin。

## 远程 pipe

`hiraku pipe` 可以代替 `tsreplace` 通过 `-e` 启动的本地 FFmpeg：它把 `tsreplace` 提供的 stdin 送入远端预配置的 FFmpeg，并把远端 stdout 交还给 `tsreplace`。完整配置见 `examples/ffmpeg-pipe-config.json`。

```sh
tsreplace.exe -i input.ts -o output.ts -e hiraku.exe pipe 127.0.0.1:40773 change-me FFMPEG-X265
```

远端配置使用独立的 `pipes`：

```json
{
  "pipes": {
    "FFMPEG-X265": {
      "command": [["ffmpeg", "-y", "-f", "mpegts", "-i", "-", "-copyts", "-start_at_zero", "-vf", "yadif", "-an", "-c:v", "libx265", "-preset", "medium", "-crf", "23", "-g", "90", "-f", "mpegts", "-"]]
    }
  }
}
```

stdin 结束后，客户端只关闭 TCP 上传方向，并继续接收 FFmpeg 刷新的剩余输出。远端 pipeline 的非零退出状态会成为 `hiraku` 的退出状态。FFmpeg 参数只能来自 `hirakud` 的本地配置；客户端只能选择 pipe 名，不能提交任意命令或参数。

`maxConcurrentPipes` 限制整个 `hirakud` 同时运行的 pipe 任务数，省略时默认为 `4`。达到上限后，新请求会立即返回 busy，不会进入等待队列。

## systemd

在调谐器主机上把 `hirakud` 作为服务常驻时，可以从 `systemd/hirakud.service` 开始调整。

```sh
sudo install -m 0755 hirakud /usr/local/bin/hirakud
sudo mkdir -p /etc/hiraku
sudo install -m 0600 examples/pt4k-config.json /etc/hiraku/config.json
sudo install -m 0644 systemd/hirakud.service /etc/systemd/system/hirakud.service
sudo systemctl daemon-reload
sudo systemctl enable --now hirakud.service
```

## 生命周期

- 每个客户端请求都会从选中的 mode 模板启动一个新的 pipeline。
- 同一个 mode 同一时间只能有一个活跃 pipeline。如果某个 mode 已经在运行，agent 会在启动本地命令之前拒绝新的同 mode 请求。
- 每个 pipe 请求也会启动独立的 pipeline；同名 pipe 请求可以并发运行，但总数受 `maxConcurrentPipes` 限制。
- 客户端断开后，该 pipeline 会在 `disconnectCloseDelaySeconds` 之后停止。
- 没有共享解码会话、fanout、idle handoff 或字节缓冲。
