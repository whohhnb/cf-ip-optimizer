// Dev-only: regenerate the screenshots referenced by README.
import puppeteer from "puppeteer-core"

const base = process.argv[2] ?? "http://127.0.0.1:18090"
const out = process.argv[3] ?? "../docs"
const email = process.argv[4] ?? "m@e.p"
const password = process.argv[5] ?? "password123"

const browser = await puppeteer.launch({
  executablePath: "C:/Program Files/Google/Chrome/Application/chrome.exe",
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

async function login() {
  await page.goto(`${base}/login`, { waitUntil: "domcontentloaded" })
  await page.evaluate((t) => localStorage.setItem("pilot_token", t), auth.token)
}

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
  await new Promise((r) => setTimeout(r, 1500))
  if (action) {
    await action()
    await new Promise((r) => setTimeout(r, 800))
  }
  await page.screenshot({ path: `${out}/${name}.png` })
  console.log("saved", name)
}

const clickByText = (text) => async () => {
  await page.evaluate((t) => {
    const b = [...document.querySelectorAll("button,[role=button]")].find((x) =>
      x.textContent?.includes(t),
    )
    b?.click()
  }, text)
}

await login()

await shot("dashboard", "/", { theme: "emerald", mode: "light" })
await shot("scan-result", "/domains/1", { theme: "emerald", mode: "light" })
await shot("settings", "/settings", { theme: "emerald", mode: "light" })
await shot("rotation", "/domains", { theme: "emerald", mode: "light", action: clickByText("添加域名") })
await shot("theme-menu", "/", { theme: "emerald", mode: "light", action: clickByText("主题与外观") })
await shot("theme-dark", "/", { theme: "teal", mode: "dark" })
await shot("theme-violet", "/", { theme: "violet", mode: "light" })
await shot("theme-azure", "/", { theme: "azure", mode: "light" })

await browser.close()
