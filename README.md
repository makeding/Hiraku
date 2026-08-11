# Hiraku

Translations: [日本語](README.ja.md) | [简体中文](README.zh.md)

Hiraku is a small relay agent for running selected tuner command templates on a remote machine and streaming the output back over TCP.

This repository provides two commands:

- `hirakud`: runs on the tuner host and exposes only the configured pipelines over TCP.
- `hiraku`: runs on the Mirakurun host, requests one configured mode from `hirakud`, and writes the received stream to stdout.

## Build

```sh
go build ./cmd/hirakud
go build ./cmd/hiraku
```

Install `hirakud` on the tuner host and `hiraku` on the Mirakurun host.

## Mirakurun

Use of the [makeding/Mirakurun](https://github.com/makeding/Mirakurun) fork is recommended.

This remote tuner setup uses different commands for `BS` / `CS` and `BS4K`, so the Mirakurun side needs `commandBS4K`. Without `commandBS4K`, Mirakurun cannot call a separate `hiraku` mode only for `BS4K`.

If you want to convert BS4K MMTS to MPEG-2 TS before returning the stream, add [makeding/hantto4k](https://github.com/makeding/hantto4k) to the pipeline on the tuner host.

Use `hiraku` as a normal Mirakurun tuner command. The mode name must match a key under `modes` in the `hirakud` config.

```yaml
command: hiraku 192.168.1.102:40773 change-me BSCS1 <channel>
commandBS4K: hiraku 192.168.1.102:40773 change-me BS4K1 <channel>
```

`BSCS1` passes the incoming channel directly to `recdvb`. `BS4K1` passes the incoming channel directly to `recdvb4k --b61`. The actual command and options are controlled by the `hirakud` config on the tuner host.

To use a mode that converts the stream with `hantto4k`, point `commandBS4K` at that conversion mode instead.

```yaml
commandBS4K: hiraku 192.168.1.102:40773 change-me BS4K1-M2TS <channel>
```

## Security

Use this project only on a trusted private network. Traffic is not encrypted; do not expose it to the public internet.

## hirakud Config

See `examples/pt4k-config.json` for a full example.

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

Tuner requests can only specify `mode` and `channel`. `mode` identifies a local command template under `modes.*.record`, and `hirakud` only expands `<channel>` inside that template. Pipe requests likewise select only a preconfigured name under `pipes`. Clients cannot send arbitrary shell commands.

`allowIPv4CidrRanges` restricts client source addresses before request authentication. When omitted, it defaults to LAN-oriented IPv4 ranges similar to Mirakurun: `10.0.0.0/8`, `127.0.0.0/8`, `172.16.0.0/12`, and `192.168.0.0/16`.

`disconnectCloseDelaySeconds` is the grace period before stopping the local recording command after the client TCP connection ends. When omitted, it defaults to `0`, which stops the command immediately after disconnect.

Pipelines are arrays of argv arrays. They are executed without a shell, and stdout of each step is piped to stdin of the next step.

## Remote pipe

`hiraku pipe` can replace the local FFmpeg process launched by `tsreplace` through `-e`. It sends the stdin produced by `tsreplace` to a preconfigured remote FFmpeg and returns remote stdout to `tsreplace`. See `examples/ffmpeg-pipe-config.json` for the complete configuration.

```sh
tsreplace.exe -i input.ts -o output.ts -e hiraku.exe pipe 127.0.0.1:40773 change-me FFMPEG-X265
```

Remote pipes use a separate `pipes` section:

```json
{
  "pipes": {
    "FFMPEG-X265": {
      "command": [["ffmpeg", "-y", "-f", "mpegts", "-i", "-", "-copyts", "-start_at_zero", "-vf", "yadif", "-an", "-c:v", "libx265", "-preset", "medium", "-crf", "23", "-g", "90", "-f", "mpegts", "-"]]
    }
  }
}
```

After stdin reaches EOF, the client closes only the TCP upload direction and continues receiving the remaining flushed output. A non-zero remote pipeline status becomes the `hiraku` exit status. FFmpeg arguments come only from the local `hirakud` configuration; clients can select a pipe name but cannot submit commands or arguments.

`maxConcurrentPipes` limits the total number of pipe tasks running in one `hirakud`. It defaults to `4` when omitted. New requests are rejected as busy immediately when the limit is reached; they are not queued.

## systemd

Use `systemd/hirakud.service` as a starting point when running `hirakud` as a service on the tuner host.

```sh
sudo install -m 0755 hirakud /usr/local/bin/hirakud
sudo mkdir -p /etc/hiraku
sudo install -m 0600 examples/pt4k-config.json /etc/hiraku/config.json
sudo install -m 0644 systemd/hirakud.service /etc/systemd/system/hirakud.service
sudo systemctl daemon-reload
sudo systemctl enable --now hirakud.service
```

## Lifecycle

- Each client request starts a new pipeline from the selected mode template.
- A mode can only have one active pipeline at a time. If the same mode is requested while it is already running, the agent rejects the request before starting local commands.
- Each pipe request also starts an independent pipeline; requests for the same pipe can run concurrently up to the global `maxConcurrentPipes` limit.
- When the client disconnects, that pipeline is stopped after `disconnectCloseDelaySeconds`.
- There is no shared decode session, fanout, idle handoff, or byte buffering.
