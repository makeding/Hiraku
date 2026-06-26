# Hiraku

Translations: [English](README.md) | [简体中文](README.zh.md)

Hiraku は、Mirakurun から離れたマシン上のチューナーコマンドを TCP 経由で呼び出すための小さなリレーエージェントです。

このリポジトリには 2 つのコマンドが含まれます。

- `hirakud`: チューナーを接続しているホストで起動し、設定済みのパイプラインだけを TCP で公開します。
- `hiraku`: Mirakurun 側で実行し、`hirakud` に指定した mode を要求して、受け取ったストリームを stdout に流します。

## ビルド

```sh
go build ./cmd/hirakud
go build ./cmd/hiraku
```

生成した `hirakud` はチューナーホストへ、`hiraku` は Mirakurun ホストへ配置してください。

## Mirakurun

Mirakurun は [makeding/Mirakurun](https://github.com/makeding/Mirakurun) の fork を使う構成を推奨します。

この構成では `BS` / `CS` と `BS4K` で別々のリモートコマンドを使うため、Mirakurun 側に `commandBS4K` が必要です。`commandBS4K` がない Mirakurun では、`BS4K` だけを別 mode に分けて `hiraku` を呼び出せません。

BS4K の MMTS を MPEG-2 TS に変換して返したい場合は、チューナーホスト側で [makeding/hantto4k](https://github.com/makeding/hantto4k) をパイプラインに入れてください。

`hiraku` は通常の Mirakurun チューナーコマンドとして指定します。mode 名は `hirakud` の設定ファイルにある `modes` のキーと一致させてください。

```yaml
command: hiraku 192.168.1.102:40773 change-me BSCS1 <channel>
commandBS4K: hiraku 192.168.1.102:40773 change-me BS4K1 <channel>
```

`BSCS1` は受け取った channel をそのまま `recdvb` に渡します。`BS4K1` は受け取った channel をそのまま `recdvb4k --b61` に渡します。実際に使うコマンドやオプションはチューナーホスト側の `hirakud` 設定で決めます。

`hantto4k` で変換する mode を使う場合は、`commandBS4K` の mode 名をその変換用 mode に差し替えます。

```yaml
commandBS4K: hiraku 192.168.1.102:40773 change-me BS4K1-M2TS <channel>
```

## hirakud の設定

設定例は `examples/pt4k-config.json` を参照してください。

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

クライアントが指定できるのは `mode` と `channel` だけです。`mode` は `modes.*.record` にあるローカルコマンドテンプレートの識別子で、`hirakud` はそのテンプレート内の `<channel>` だけを置換します。Mirakurun ホストから任意のシェルコマンドを送ることはできません。

`allowIPv4CidrRanges` は、リクエスト認証より前にクライアントの接続元 IPv4 アドレスを制限します。省略時は Mirakurun と同じような LAN 向けの許可リストとして、`10.0.0.0/8`、`127.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16` が使われます。

`disconnectCloseDelaySeconds` は、クライアントの TCP 接続が切れたあと、ローカルの録画コマンドを停止するまでの猶予秒数です。省略時は `0` で、切断後すぐに停止します。

パイプラインは argv 配列の配列です。シェルは使わず、各ステップの stdout が次のステップの stdin に接続されます。

## systemd

チューナーホストで `hirakud` を常駐させる場合は `systemd/hirakud.service` を参考にしてください。

```sh
sudo install -m 0755 hirakud /usr/local/bin/hirakud
sudo mkdir -p /etc/hiraku
sudo install -m 0600 examples/pt4k-config.json /etc/hiraku/config.json
sudo install -m 0644 systemd/hirakud.service /etc/systemd/system/hirakud.service
sudo systemctl daemon-reload
sudo systemctl enable --now hirakud.service
```

## ライフサイクル

- クライアントからの各リクエストごとに、選択された mode のパイプラインを新しく起動します。
- 同じ mode は同時に 1 つのパイプラインだけを実行できます。すでに実行中の mode が要求された場合、ローカルコマンドを起動する前に拒否します。
- クライアントが切断されると、そのパイプラインは `disconnectCloseDelaySeconds` の後に停止されます。
- 共有デコードセッション、fanout、idle handoff、バイトバッファリングはありません。
