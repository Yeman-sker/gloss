# 动态图标

Gloss 的动态图标从这里生成。`src/`、`index.html`、`scripts/check.mjs`、`scripts/export.mjs` 和 `LICENSE` 来自 Kubai Icons 1.0.1，按其许可证第 3 条复制，每个文件保留原版权声明，内容未改。`gloss-icons.json` 和 `scripts/bake.mjs` 是我们自己的。

| 文件 | 作用 |
|---|---|
| `gloss-icons.json` | 用到哪些图标：`id` 是 `app/icons/` 里的文件名，`icon` 是引擎里的图标名，`accent` 是主色（按物体的材质取），`chroma` 可选，用来压低同样明度下显得更艳的色相 |
| `scripts/bake.mjs` | 用 Chrome 跑引擎，把每个图标的悬停动效取样成多帧 SVG，按暗色规则调色，写成 `app/icons/<id>.svgz` |

```bash
make icons
```

需要 Node 18 以上和 Chrome（可用 `CHROME` 指定路径）。结果已入库，构建和运行都不需要它们。

改完要到 mygo 里核对：mygo 和 Chrome 的渲染有差异（比如零长度的线会画成圆点）。为什么这么做见[决策 0012](../../docs/decisions/0012-baked-icons.md)。
