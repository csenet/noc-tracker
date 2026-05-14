# NOC Tracker

Aruba Instant On (クラウドポータル) と Aruba IAP (ローカル SSH) を両方サマって、
登録した MAC アドレスがいまどの AP に繋がっているかをブラウザから見られるツール。
ソース IP からアクセス元の端末を自動同定する `/api/me` も付いてる。

> **このサービスに認証はない** — LAN内なら誰でも他人の位置が見えるから、信頼できる
> ネットワーク内でだけ動かしてや。

## 必要なもの

- Go 1.25 以上 (この repo は aqua で `golang/go@go1.25.6` を pin している)
- Aruba Instant On のクラウドポータルアカウント (任意)
- Aruba IAP の SSH 到達性 + admin パスワード (任意) **+ `expect(1)` バイナリ**
  (Aruba IAP は exec チャネル拒否 + プロンプト連動が必須なので、
  `expect` で `#` プロンプト待ちしながらコマンドを順送りする。macOS なら
  `/usr/bin/expect` が標準で入ってる。Linux は `apt install expect` / `dnf install expect`)
- 少なくとも片方のソースは有効にしとかんと、ただの登録帳になる

## セットアップ

```bash
cp .env.example .env
# .env を編集
go run .
```

ブラウザで <http://localhost:8080> 開く。

## 環境変数

| 変数 | デフォルト | 説明 |
| ---- | ----- | ---- |
| `ARUBA_USERNAME` / `ARUBA_PASSWORD` | (空) | Instant On クラウドポータルの ID/PW。両方セットすると有効化 |
| `IAP_HOST` | (空) | IAP の IP/ホスト名。空ならこのソースは無効 |
| `IAP_PORT` | `22` | IAP SSH ポート |
| `IAP_USERNAME` | `admin` | IAP SSH ユーザー |
| `IAP_PASSWORD` | (空) | IAP SSH パスワード |
| `NOC_LISTEN` | `:8080` | HTTP リスンアドレス |
| `NOC_STORE_PATH` | `registrations.json` | 登録 MAC を保存する JSON ファイル |
| `NOC_AP_POSITIONS_PATH` | `ap-positions.json` | 会場図上の AP 座標を保存する JSON ファイル |
| `NOC_POLL_INTERVAL` | `30s` | ソースを再取得する間隔 (例: `30s`, `1m`, `90`) |
| `NOC_ADMIN_TOKEN` | (空) | 設定すると AP 座標編集に管理者トークンが必要になる |

## API

| メソッド | パス | 内容 |
| ---- | ---- | ---- |
| GET | `/api/status` | 最終ポーリング時刻 / クライアント数 / 直近エラー |
| GET | `/api/clients` | 登録済 MAC + 現在オンラインのクライアントをマージした一覧 |
| GET | `/api/registrations` | 登録済 MAC の生データ |
| POST | `/api/registrations` | `{ "mac": "...", "name": "...", "owner": "..." }` で登録 |
| DELETE | `/api/registrations/{mac}` | 登録を削除 |
| GET | `/api/me?ip=...` | 引数 IP (省略時はアクセス元 IP) から MAC を逆引きして位置を返す |
| GET | `/api/aps` | いま観測されている AP 名の一覧 (ソート済) |
| GET | `/api/ap-positions` | 会場図上の AP 座標マップ `{ "<ap-name>": {"x":N,"y":N} }` |
| POST | `/api/ap-positions` | 上記の **全部を上書き** で保存 (部分更新ではない) — 管理者専用 (※) |
| GET | `/api/auth` | `{ "admin_required": bool, "is_admin": bool }` を返す |

(※) `NOC_ADMIN_TOKEN` をセットしている場合のみ。リクエストヘッダ `X-Admin-Token: <token>` で送信する。

MAC は正規化されて `aa:bb:cc:dd:ee:ff` 形式で保存される。`AA-BB-CC-DD-EE-FF`,
`AABBCCDDEEFF`, `aabb.ccdd.eeff` のどれを入れても OK。

## 会場マップ (Floorplan)

会場図画像の上に AP のドットを置いて、登録ユーザがどこに居るかを見える化できる。

1. **画像を置く**: 会場図を `web/static/floorplan.{png,jpg,jpeg,webp,svg}` のどれかで
   保存してサーバ再起動。サーバが自動で拡張子を探して `/floorplan` URLで配信する。
   画像の自然サイズが SVG の座標系になる (例: 1600x900 の画像なら座標も 1600x900)。

2. **AP を配置**: Web UI の「会場マップ」セクションで `編集モード` をオン →
   左上に積み上がっている AP ドットをドラッグして所定の位置に置く →
   `座標を保存` で `ap-positions.json` に永続化。

3. **見方**:
   - **集計**: AP に繋がっている登録ユーザの人数をドット直径と中央の数字で表示。
     ドットをクリックすると誰が居るかリスト表示。
   - **個別**: 登録ユーザ1人ずつのピンを AP の近くに散らす ("Find my friends"風)。
     ヘッダの `集計 | 個別` トグルで切り替え。

座標が `ap-positions.json` だけに依存しているので、AP を増やしてもサーバ再起動なしで
左上に新しいドットが現れる。位置だけ調整して保存すればOK。

### 管理者モード (AP編集ロック)

会場本番で「他人にAP配置をいじられたくない」場合は `NOC_ADMIN_TOKEN` を設定する:

```bash
NOC_ADMIN_TOKEN=hogehoge123 go run .
```

これで「編集モード」チェックボックスと「座標を保存」ボタンが、トークン持ちにだけ表示される。
ログイン手順:

1. 管理者は `http://host:8080/?admin=hogehoge123` を一度開く
2. ブラウザの localStorage にトークンが保存され、URLからは削除される
3. 以降は普通に `http://host:8080/` を開いて編集できる
4. ログアウトは画面上の「ログアウト」ボタン (localStorage から消す)

トークン未設定 (デフォルト) なら誰でも編集可能 — 既存挙動を壊さないため opt-in。

## Binary で動かす (推奨)

毎 push で GitHub Actions が `linux/amd64`, `linux/arm64`, `darwin/amd64`,
`darwin/arm64` の static binary を生成する:

- **main push**: Actions の Run ページ → Artifacts に出る (14日保持)
- **タグ push** (`v1.0.0` など): [GitHub Releases](https://github.com/csenet/noc-tracker/releases)
  に upload される

```bash
# 例: タグ release から
curl -L https://github.com/csenet/noc-tracker/releases/latest/download/noc-tracker_linux_amd64.tar.gz | tar xz
cp .env.example .env  # 値を編集
./noc-tracker_linux_amd64
```

⚠️ **IAP source を使うなら `expect(1)` が PATH に必要** (`apt install expect` /
`brew install expect`)。Instant On のみで動かす場合は不要。

## Docker で動かす

`expect(1)` も同梱した alpine ベースのイメージを `ghcr.io/csenet/noc-tracker:latest`
で公開している (push されると GitHub Actions が自動ビルド)。

```bash
# 1. data ディレクトリと .env を準備
mkdir -p data
cp .env.example .env  # 値を編集
cp /path/to/floorplan.jpg data/

# 2. 起動 (image を pull)
docker compose up -d

# あるいはローカルでビルド
docker compose up -d --build
```

永続化:

- `./data/registrations.json` 登録した端末
- `./data/ap-positions.json` AP 座標
- `./data/floorplan.{png,jpg,jpeg,webp,svg}` 会場図画像 (差し替え自由)

`NOC_FLOORPLAN_DIR` 環境変数を使うと、 イメージに焼き込まれた画像より
このディレクトリを優先する設計になっている。なので画像差し替えだけならコンテナ
再ビルド不要。

### ネットワーク設定

デフォルトの `docker-compose.yml` は **Linux 本番運用** を想定して
`network_mode: host` を使う。理由はソースIPがそのままホストのNIC由来
になるので `/api/me` の IP マッチが効きやすいから。

| 環境 | 設定 |
| --- | --- |
| **Linux サーバ (本番)** | `network_mode: host` のまま (デフォルト)。`8080` がホストの `8080` で公開される |
| **Docker Desktop on Mac / Windows** | `network_mode: host` を消して `ports: ["8080:8080"]` に差し替え。host networking は Mac/Win では no-op で警告が出るだけ |

VPN や別 NIC 経由でアクセスする場合は `network_mode: host` でも IP マッチ
しないので、Web UI の「あなたの位置」で **端末を MAC で選ぶ**
(`?mac=aa:bb:cc:dd:ee:ff` でも可) のが確実。

## 設計メモ

- **状態は in-memory + JSON ファイル**。履歴は持たない。「いま」の位置が分かることが目的。
- **同じ MAC が両ソースに見えた場合は Instant On 優先**。SSID / 信号強度などの情報量が多いから。
- **AP 名 = 場所ラベル**。Instant On / IAP 側で `1F-Entrance` みたいに分かりやすい名前を付けとくと、そのまま表示される。

## トラブルシューティング

### `/api/me` が見つからない
- 別 NIC (VPN / モバイル回線) 経由でアクセスしてる場合、ソース IP が Wi-Fi の IP と一致しない。Wi-Fi で直接サーバにアクセスして。
- リバプロ越しなら `X-Forwarded-For` を読んでる。プロキシ側で正しいヘッダを立てて。

### IAP の `show clients` の出力が空 / カラムずれ
- IAP は `ssh user@host 'show clients'` のような exec チャネル接続を
  `Only cli connections are allowed to the AP` で蹴ってくる。さらに PTY を取れたとしても
  stdin にコマンドを一気に流すと CLI が処理する前に EOF が来て無視されるので、
  `expect(1)` で `#` プロンプト待ちをはさみながら `show clients` を順送りしている。
- ArubaOS Instant 8.x では `no paging` は `% Parse error` になるので投げてない。
  478クライアントぶら下がっとる実機で `ssh -tt` で pager に当たらず全行流れることは確認済み。
- **パーサのカラム名は実機未検証**。ヘッダ名 (`Access Point` vs `AP Name`) や
  信号表記 (`63(good)` vs `-63`) はファーム世代で変わる。動かんかったら生出力を取って
  確認してみて:

  ```bash
  IAP_HOST=192.168.0.80 IAP_PASSWORD='...' go run ./cmd/iap-dump
  # パーサが何を拾ったか見たいとき:
  IAP_HOST=192.168.0.80 IAP_PASSWORD='...' go run ./cmd/iap-dump -parse
  ```

  ずれてたら `iap/parser_test.go` の `sampleOutput` を実機データに置き換えて
  `go test ./iap` でリグレッション確認。
- パスワード認証が無効化されている (公開鍵のみ) ケースは未対応。

### Instant On の MFA 必須
- 既存の [aruba-instant-on-exporter](../aruba-instant-on-exporter) と同じ OAuth2/PKCE フローを使っている。アカウントが MFA 必須だと初回認証に失敗する。
