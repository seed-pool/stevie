import { mkdirSync, writeFileSync, existsSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const dir = join(dirname(fileURLToPath(import.meta.url)), '..', 'vendor')
mkdirSync(dir, { recursive: true })
const assets = [
  ['lock.wasm', 'https://strmd.b-cdn.net/js/wasm/lock.wasm'],
  ['lock.js', 'https://strmd.b-cdn.net/js/wasm/lock.js'],
]
for (const [name, url] of assets) {
  const dest = join(dir, name)
  if (existsSync(dest)) continue
  const embed = (process.env.STEVIE_STREAMED_EMBED_URL || 'https://embed.st').replace(/\/$/, '')
  const res = await fetch(url, {
    headers: { Referer: `${embed}/`, 'User-Agent': 'StevieSports/1.0' },
  })
  if (!res.ok) throw new Error(`fetch ${name}: ${res.status}`)
  writeFileSync(dest, Buffer.from(await res.arrayBuffer()))
  console.log('vendor', name, 'ok')
}
