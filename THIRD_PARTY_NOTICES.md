# 第三方组件声明 · Third-Party Notices

EdgePilot 本体以 [AGPL-3.0](LICENSE) 授权。下表列出随项目分发的第三方组件及其许可，
均为 AGPL-3.0 兼容的宽松许可（MIT / BSD / ISC / Apache-2.0 / MPL-2.0 / OFL-1.1）。

## 后端（Go）

| 组件 | 许可 | 用途 |
| --- | --- | --- |
| [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) | BSD-3-Clause | 纯 Go 的 SQLite 驱动 |
| [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) | BSD-3-Clause | bcrypt 口令哈希 |
| [github.com/google/uuid](https://github.com/google/uuid) | BSD-3-Clause | modernc.org/sqlite 间接依赖 |
| [github.com/dustin/go-humanize](https://github.com/dustin/go-humanize) | BSD-3-Clause | modernc.org/sqlite 间接依赖 |
| [github.com/ncruces/go-strftime](https://github.com/ncruces/go-strftime) | MIT | modernc.org/sqlite 间接依赖 |
| [github.com/mattn/go-isatty](https://github.com/mattn/go-isatty) | MIT | modernc.org/sqlite 间接依赖 |
| [github.com/hashicorp/golang-lru/v2](https://github.com/hashicorp/golang-lru) | **MPL-2.0** | modernc.org/sqlite 间接依赖 |

> `hashicorp/golang-lru/v2` 是唯一带弱著佐权的依赖。按 MPL-2.0 §3.3，它可以并入
> GPL/AGPL 作品；本项目未修改其源码。若后续修改了该库的文件，需按 MPL-2.0 公开该文件的改动。

其余 `golang.org/x/*`、`modernc.org/*`（libc、memory、mathutil、cc、ccgo 等）均为 BSD-3-Clause。
完整依赖树见 `backend/go.sum`。

## 前端（React）

| 组件 | 许可 | 用途 |
| --- | --- | --- |
| [React](https://github.com/facebook/react) / React DOM | MIT | UI 运行时 |
| [Base UI](https://base-ui.com/) (`@base-ui/react`) | MIT | shadcn base-nova 的底层无障碍原语 |
| [shadcn/ui](https://ui.shadcn.com/) | MIT | `web/src/components/ui/**` 为 registry 拷贝进来的组件源码 |
| [Tailwind CSS](https://tailwindcss.com/) | MIT | 样式引擎 |
| [Recharts](https://recharts.org/) | MIT | 趋势图表 |
| [Sonner](https://sonner.emilkowal.ski/) | MIT | Toast 通知 |
| [next-themes](https://github.com/pacocoursey/next-themes) | MIT | 明暗模式 |
| [React Router](https://reactrouter.com/) | MIT | 前端路由 |
| [Lucide](https://lucide.dev/) | ISC | 图标 |
| [class-variance-authority](https://github.com/joe-bell/cva) | Apache-2.0 | 组件变体定义 |
| [Geist 字体](https://vercel.com/font) (`@fontsource-variable/geist`) | OFL-1.1 | 界面字体，随包内嵌 |

> `web/src/components/ui/` 下的文件来自 shadcn/ui registry，遵循其 MIT 许可；
> 本项目对其做了主题化调整，原始版权归 shadcn/ui 作者所有。

## 参考资料

| 项目 | 许可 | 说明 |
| --- | --- | --- |
| [jeessy2/ddns-go](https://github.com/jeessy2/ddns-go) | MIT | 各家 DNS 服务商的接口调用流程与签名算法参照其实现，代码为本项目独立编写 |
| [Cloudflare 官方地址段](https://www.cloudflare.com/ips/) | — | 扫描候选 IP 的来源数据 |

## 许可兼容性说明

- MIT / BSD / ISC：可自由并入 AGPL-3.0 作品，保留原版权声明即可
- Apache-2.0：与 GPLv3 / AGPLv3 单向兼容（Apache 2.0 §3 已授予专利许可，无需额外条款）
- MPL-2.0：文件级弱著佐权，允许并入 GPL/AGPL 作品（§3.3）
- OFL-1.1：字体专用许可，允许随软件嵌入分发，不得单独售卖字体本身

如需以闭源方式商用本项目，请先取得作者商业授权，并自行复核上述依赖在你的分发形态下是否合规。
