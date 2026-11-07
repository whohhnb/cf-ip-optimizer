// 重新生成 README 里的截图（开发用，不进 CI）。
//
// 需要一个已登录过、且有演示数据的实例：
//   PILOT_DATA_DIR=./demo ./dist/edgepilot &
//   DEMO_EMAIL=admin@example.com DEMO_PASS=*** node scripts/shoot-docs.mjs http://127.0.0.1:8080 ../docs
//
// 凭据只从环境变量读，不写死在仓库里。
import puppeteer from "puppeteer-core"

const base = process.argv[2] ?? "http://127.0.0.1:8080"
const out = process.argv[3] ?? "../docs"
const email = process.env.DEMO_EMAIL
const password = process.env.DEMO_PASS

if (!email || !password) {
  console.error("缺少演示账号：请设置 DEMO_EMAIL / DEMO_PASS 环境变量")
  process.exit(1)
}

const browser = await puppeteer.launch({
  executablePath: process.env.CHROME_PATH ?? "C:/Program Files/Google/Chrome/Application/chrome.exe",
  headless: true,
  args: ["--no-sandbox", "--disable-gpu"],
})
const page = await browser.newPage()
page.on("pageerror", (e) => console.log("[pageerror]", e.message))

const res = await fetch(`${base}/api/auth/login`, {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ email, password }),
})
const auth = await res.json()
if (!auth.token) {
  console.error("登录失败：", JSON.stringify(auth))
  process.exit(1)
}

/** 截图前先写入 token / 配色 / 明暗，再导航，避免闪一下默认主题。 */
async function shot(name, route, { width = 1440, height = 1000, theme, mode, action } = {}) {
  await page.setViewport({ width, height, deviceScaleFactor: 1 })
  await page.goto(`${base}/login`, { waitUntil: "domcontentloaded" })
  await page.evaluate(
    (t, th, m) => {
      localStorage.setItem("pilot_token", t)
      if (th) localStorage.setItem("edgepilot.color", th)
      if (m) localStorage.setItem("theme", m)
    },
    auth.token,
    theme,
    mode,
  )
  await page.goto(base + route, { waitUntil: "domcontentloaded" })
  await new Promise((r) => setTimeout(r, 1600))
  if (action) {
    await action()
    await new Promise((r) => setTimeout(r, 800))
  }
  await page.screenshot({ path: `${out}/${name}.png` })
  console.log("saved", name)
}

const clickText = (text) => async () => {
  await page.evaluate((t) => {
    const b = [...document.querySelectorAll("button,[role=button]")].find((x) =>
      x.textContent?.includes(t),
    )
    b?.click()
  }, text)
}

await shot("dashboard", "/", { theme: "emerald", mode: "light" })
await shot("scan-result", "/domains/1", { theme: "emerald", mode: "light" })
await shot("rotation", "/domains", {
  theme: "emerald",
  mode: "light",
  action: clickText("添加域名"),
})
await shot("theme-menu", "/", {
  theme: "emerald",
  mode: "light",
  action: clickText("主题与外观"),
})
await shot("theme-dark", "/", { theme: "teal", mode: "dark" })

await browser.close()
