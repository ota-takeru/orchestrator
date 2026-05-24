# DevOS UI

Human Inboxを中心にしたReact UIです。状態のsource of truthはGo APIで、UIは `/api/ui/snapshot`、`/api/inbox`、`/api/decisions`、`/api/memory` を読みます。

## Commands

```text
pnpm --dir ui install
pnpm --dir ui dev
pnpm --dir ui test
pnpm --dir ui build
```

開発時は別プロセスで `devos serve --addr 127.0.0.1:8765` を起動します。Vite dev serverは `/api` をGo APIへproxyします。
