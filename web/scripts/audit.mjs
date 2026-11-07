// Dev-only: style audit — captures the app across viewports and sidebar states.
import puppeteer from "puppeteer-core"

const base = process.argv[2] ?? "http://127.0.0.1:18090"
const out = process.argv[3] ?? "./shots-audit"
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

async function shot(name, route, width, height, collapse = false) {
  await page.setViewport({ width, height, deviceScaleFactor: 1 })
  await page.goto(`${base}${route}`, { waitUntil: "domcontentloaded" })
  await page.evaluate((t) => localStorage.setItem("pilot_token", t), auth.token)
  await page.goto(`${base}${route}`, { waitUntil: "domcontentloaded" })
  await new Promise((r) => setTimeout(r, 1300))
  if (collapse) {
    await page.evaluate(() => {
      const btn = document.querySelector("[data-slot=sidebar-trigger]")
      btn?.click()
    })
    await new Promise((r) => setTimeout(r, 700))
  }
  await page.screenshot({ path: `${out}/${name}.png` })
  console.log("saved", name)
}

await shot("01-collapsed-1440", "/", 1440, 900, true)
await shot("02-expanded-1440", "/", 1440, 900)
await shot("03-tablet-1024", "/domains", 1024, 800)
await shot("04-tablet-768", "/domains/1", 768, 900)
await shot("05-settings-1440", "/settings", 1440, 1000)
await shot("06-accounts-1440", "/accounts", 1440, 900)
await shot("07-logs-1440", "/logs", 1440, 900)

await browser.close()
