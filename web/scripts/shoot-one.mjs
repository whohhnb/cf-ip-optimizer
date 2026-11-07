// Dev-only: screenshot a single authenticated route.
// Usage: node scripts/shoot-one.mjs <baseUrl> <email> <password> <route> <outFile>
import puppeteer from "puppeteer-core"

const [base, email, password, route, out] = process.argv.slice(2)

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
  body: JSON.stringify({ email, password }),
})
const auth = await res.json()
await page.goto(`${base}/login`, { waitUntil: "domcontentloaded" })
await page.evaluate((t) => localStorage.setItem("pilot_token", t), auth.token)
await page.goto(base + route, { waitUntil: "domcontentloaded" })
await new Promise((r) => setTimeout(r, 2000))
await page.screenshot({ path: out, fullPage: true })
await browser.close()
console.log("saved", out)
