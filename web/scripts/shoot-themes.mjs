// Dev-only: capture the same page under several MD3 color themes.
import puppeteer from "puppeteer-core"

const base = process.argv[2] ?? "http://127.0.0.1:18085"
const outDir = process.argv[3] ?? "./shots"

const themes = [
  ["violet", "light"],
  ["azure", "light"],
  ["emerald", "light"],
  ["rose", "light"],
  ["amber", "light"],
  ["teal", "dark"],
]

const browser = await puppeteer.launch({
  executablePath: "C:/Program Files/Google/Chrome/Application/chrome.exe",
  headless: true,
  args: ["--no-sandbox", "--disable-gpu"],
  defaultViewport: { width: 1440, height: 1000 },
})
const page = await browser.newPage()
page.on("pageerror", (e) => console.log("[pageerror]", e.message))

const res = await fetch(`${base}/api/auth/login`, {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ email: "demo@edge.pilot", password: "password123" }),
})
const auth = await res.json()

await page.goto(`${base}/login`, { waitUntil: "domcontentloaded" })
await page.evaluate((t) => localStorage.setItem("pilot_token", t), auth.token)

for (const [theme, mode] of themes) {
  await page.evaluate(
    (t, m) => {
      localStorage.setItem("edgepilot.color", t)
      localStorage.setItem("theme", m)
    },
    theme,
    mode,
  )
  await page.goto(`${base}/`, { waitUntil: "domcontentloaded" })
  await page.evaluate((t, m) => {
    document.documentElement.dataset.theme = t
    document.documentElement.classList.toggle("dark", m === "dark")
  }, theme, mode)
  await new Promise((r) => setTimeout(r, 1400))
  await page.screenshot({ path: `${outDir}/theme-${theme}-${mode}.png` })
  console.log("saved", theme, mode)
}

await browser.close()
