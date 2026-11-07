// Dev-only visual smoke test: renders key pages headlessly and saves PNGs.
// Usage: node scripts/shoot.mjs <baseUrl> <email> <password> <outDir>
import fs from "node:fs/promises"
import path from "node:path"
import puppeteer from "puppeteer-core"

const [base = "http://127.0.0.1:18080", email, password, outDir = "./shots"] = process.argv.slice(2)

const chrome = "C:/Program Files/Google/Chrome/Application/chrome.exe"

const browser = await puppeteer.launch({
  executablePath: chrome,
  headless: true,
  args: ["--no-sandbox", "--disable-gpu", "--force-device-scale-factor=1"],
  defaultViewport: { width: 1440, height: 960 },
})

const page = await browser.newPage()
page.on("console", (m) => {
  if (m.type() === "error") console.log("[console]", m.text())
})
page.on("pageerror", (e) => console.log("[pageerror]", e.message))

await fs.mkdir(outDir, { recursive: true })

// 1. login page
await page.goto(`${base}/login`, { waitUntil: "domcontentloaded" })
await page.screenshot({ path: path.join(outDir, "01-login.png") })

// 2. authenticate through the real API, then inject the token
const res = await fetch(`${base}/api/auth/login`, {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ email, password }),
})
const auth = await res.json()
if (!auth.token) throw new Error("login failed: " + JSON.stringify(auth))

await page.evaluate((t) => localStorage.setItem("pilot_token", t), auth.token)

for (const [name, route] of [
  ["02-dashboard", "/"],
  ["03-domains", "/domains"],
  ["04-accounts", "/accounts"],
  ["05-settings", "/settings"],
  ["06-logs", "/logs"],
]) {
  await page.goto(base + route, { waitUntil: "domcontentloaded" })
  await new Promise((r) => setTimeout(r, 600))
  await page.screenshot({ path: path.join(outDir, `${name}.png`), fullPage: true })
}

await browser.close()
console.log("done ->", outDir)
