# Hiraku

Small command relay agent for exposing selected local command templates over TCP.

This project provides two commands:

- `hirakud`: runs on the command host and exposes configured pipelines over TCP.
- `hiraku`: requests one configured mode from `hirakud` and writes the remote stream to stdout.

## Build

```sh
go build ./cmd/hirakud
go build ./cmd/hiraku
```

## Mirakurun tuner command

Use `hiraku` as a normal Mirakurun tuner command.

```yaml
command: hiraku 192.168.1.102:40773 change-me BSCS <channel>
commandBS4K: hiraku 192.168.1.102:40773 change-me BS4K <channel>
```

`BSCS` passes the incoming channel directly to `recdvb`. `BS4K` passes it directly to `recdvb4k --acas` without an `hantto4k` conversion step.

## Agent config

See `examples/pt4k-config.json`.

The client can only request `mode` and `channel`. `mode` is the identity of a local command template under `modes.*.record`; the agent expands `<channel>` inside that template, so the Mirakurun host cannot send arbitrary shell commands.

`allowIPv4CidrRanges` restricts client source addresses before request authentication, using the same IPv4 CIDR allow-list style as Mirakurun. When omitted, it defaults to `10.0.0.0/8`, `127.0.0.0/8`, `172.16.0.0/12`, and `192.168.0.0/16`.

`disconnectCloseDelaySeconds` delays stopping the local command pipeline after the client TCP connection ends. When omitted, it defaults to `0`, which keeps the old immediate-stop behavior.

Pipelines are arrays of argv arrays. They are executed without a shell, and stdout of each step is piped to stdin of the next step.

## Lifecycle

- Each client request starts its own pipeline from the selected mode template.
- A mode can only have one active pipeline at a time. If the same mode is requested while it is already running, the agent rejects the request before starting local commands.
- When the client disconnects, that pipeline is stopped after `disconnectCloseDelaySeconds`.
- There is no shared decode session, fanout, idle handoff, or byte buffering.
