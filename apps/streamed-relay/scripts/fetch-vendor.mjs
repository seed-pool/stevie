import { mkdirSync, writeFileSync, existsSync, unlinkSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const dir = join(dirname(fileURLToPath(import.meta.url)), '..', 'vendor')
mkdirSync(dir, { recursive: true })

// Drop legacy streamed.pk / embed.st unlock binaries if present.
for (const stale of ['lock.wasm', 'lock.js']) {
  const p = join(dir, stale)
  if (existsSync(p)) {
    try {
      unlinkSync(p)
    } catch {
      /* ignore */
    }
  }
}

const embed = (process.env.STEVIE_STREAMED_EMBED_URL || 'https://taifood-blog.asia').replace(/\/$/, '')
const assets = [
  ['gasm.wasm', `${embed}/js/wasm/gasm.wasm`],
  ['gasm.js', `${embed}/js/wasm/gasm.js`],
]
for (const [name, url] of assets) {
  const dest = join(dir, name)
  if (existsSync(dest) && process.env.STEVIE_STREAMED_FORCE_VENDOR !== '1') continue
  const res = await fetch(url, {
    headers: { Referer: `${embed}/`, 'User-Agent': 'StevieSports/1.0' },
  })
  if (!res.ok) throw new Error(`fetch ${name}: ${res.status}`)
  writeFileSync(dest, Buffer.from(await res.arrayBuffer()))
  console.log('vendor', name, 'ok')
}
