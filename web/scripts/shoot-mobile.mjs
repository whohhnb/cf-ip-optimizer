// Dev-only: capture the app at a phone viewport.
import puppeteer from "puppeteer-core"

const base = process.argv[2] ?? "http://127.0.0.1:18090"
const out = process.argv[3] ?? "./shots-mobile"
const email = process.argv[4] ?? "m@e.p"
const password = process.argv[5] ?? "password123"

const routes = [
  ["login", "/login", false],
  ["dashboard", "/", true],
  ["domains", "/domains", true],
  ["detail", "/domains/1", true],
  ["accounts", "/accounts", true],
  ["settings", "/settings", true],
  ["logs", "/logs", true],
]

const browser = await puppeteer.launch({
  executablePath: "C:/Program Files/Google/Chrome/Application/chrome.exe",
  headless: true,
  args: ["--no-sandbox", "--disable-gpu"],
  defaultViewport: { width: 390, height: 844, deviceScaleFactor: 2, isMobile: true, hasTouch: true },
})
const page = await browser.newPage()
page.on("pageerror", (e) => console.log("[pageerror]", e.message))

const res = await fetch(`${base}/api/auth/login`, {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ email, password }),
})
const auth = await res.json()

for (const [name, route, needAuth] of routes) {
  await page.goto(`${base}${route}`, { waitUntil: "domcontentloaded" })
  if (needAuth) {
    await page.evaluate((t) => localStorage.setItem("pilot_token", t), auth.token)
    await page.goto(`${base}${route}`, { waitUntil: "domcontentloaded" })
  }
  await new Promise((r) => setTimeout(r, 1600))
  await page.screenshot({ path: `${out}/${name}.png`, fullPage: true })
  console.log("saved", name)
}

await browser.close()
