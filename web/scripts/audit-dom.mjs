// 布局回归检查（开发用）：在 8 个视口宽度下逐页扫描，报告
// 元素溢出视口、内容被裁剪、点击目标过小这三类问题。
//
//   DEMO_EMAIL=admin@example.com DEMO_PASS=*** node scripts/audit-dom.mjs http://127.0.0.1:8080
import puppeteer from "puppeteer-core"

const base = process.argv[2] ?? "http://127.0.0.1:8080"
const email = process.env.DEMO_EMAIL
const password = process.env.DEMO_PASS

if (!email || !password) {
  console.error("缺少演示账号：请设置 DEMO_EMAIL / DEMO_PASS 环境变量")
  process.exit(1)
}

const pages = [
  ["/", "dashboard"],
  ["/domains", "domains"],
  ["/domains/1", "detail"],
  ["/accounts", "accounts"],
  ["/settings", "settings"],
  ["/logs", "logs"],
]
const widths = [1440, 1180, 1024, 900, 768, 640, 480, 390]

const browser = await puppeteer.launch({
  executablePath: process.env.CHROME_PATH ?? "C:/Program Files/Google/Chrome/Application/chrome.exe",
  headless: true,
  args: ["--no-sandbox", "--disable-gpu"],
})
const page = await browser.newPage()

const res = await fetch(`${base}/api/auth/login`, {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ email, password }),
})
const auth = await res.json()

await page.goto(`${base}/login`, { waitUntil: "domcontentloaded" })
await page.evaluate((t) => localStorage.setItem("pilot_token", t), auth.token)

const findings = []

for (const width of widths) {
  await page.setViewport({ width, height: 900 })
  for (const [route, label] of pages) {
    await page.goto(base + route, { waitUntil: "domcontentloaded" })
    await new Promise((r) => setTimeout(r, 900))

    const issues = await page.evaluate(() => {
      const out = []
      const describe = (el) => {
        const cls = (el.getAttribute("class") || "").slice(0, 80)
        const slot = el.getAttribute("data-slot") || el.tagName.toLowerCase()
        const text = (el.textContent || "").trim().slice(0, 24)
        return `${slot}${cls ? "." + cls.split(/\s+/).slice(0, 3).join(".") : ""} «${text}»`
      }

      // 1) 溢出视口
      for (const el of document.querySelectorAll("body *")) {
        if (el.classList.contains("sr-only")) continue
        const r = el.getBoundingClientRect()
        if (r.width === 0 || r.height === 0) continue
        if (r.right > window.innerWidth + 1.5) {
          if (el.closest("[data-slot=table-container]")) continue
          out.push(`viewport-overflow x=${Math.round(r.right)} ${describe(el)}`)
        }
      }

      // 2) 内容被裁剪且没有省略号
      for (const el of document.querySelectorAll("h1,h2,h3,p,span,td,div")) {
        if (el.classList.contains("sr-only")) continue
        if (el.scrollWidth > el.clientWidth + 2 && el.clientWidth > 0) {
          const cs = getComputedStyle(el)
          if (cs.overflowX === "visible") continue
          if (el.closest("[data-slot=table-container]")) continue
          if (cs.textOverflow === "ellipsis") continue
          out.push(`clipped ${el.scrollWidth}>${el.clientWidth} ${describe(el)}`)
        }
      }

      // 3) 触控目标过小
      for (const el of document.querySelectorAll("button,[role=button],a")) {
        const r = el.getBoundingClientRect()
        if (r.height > 0 && r.height < 24 && r.width > 0) {
          out.push(`tiny-target ${Math.round(r.width)}x${Math.round(r.height)} ${describe(el)}`)
        }
      }

      return [...new Set(out)].slice(0, 12)
    })

    if (issues.length) findings.push({ width, page: label, issues })
  }
}

for (const f of findings) {
  console.log(`\n[${f.width}px] ${f.page}`)
  for (const i of f.issues) console.log("   -", i)
}
if (!findings.length) console.log("没有发现布局问题")

await browser.close()
