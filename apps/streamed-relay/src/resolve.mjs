import fs from 'node:fs'
import { resolve as absPath } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { Window } from 'happy-dom'

const UA =
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36'
const API_BASE = (process.env.STEVIE_STREAMED_BASE_URL || 'https://api.ppv.st').replace(/\/$/, '')
const WASM_DIR = fileURLToPath(new URL('../vendor', import.meta.url))
const WASM_JS = pathToFileURL(`${WASM_DIR}/gasm.js`).href
const WASM_BIN = `${WASM_DIR}/gasm.wasm`
const nativeFetch = globalThis.fetch.bind(globalThis)

let wasmLoadGeneration = 0

async function loadWasmModule() {
  wasmLoadGeneration += 1
  return import(`${WASM_JS}?${wasmLoadGeneration}`)
}

function encodeFetchBody(path) {
  const bytes = Buffer.from(path, 'utf8')
  return Buffer.concat([Buffer.from([0x0a, bytes.length]), bytes])
}

function embedFromIframe(iframe) {
  const url = new URL(String(iframe || '').trim())
  const marker = '/embed/'
  const idx = url.pathname.indexOf(marker)
  if (idx < 0) throw new Error('embed source missing /embed/ path')
  const path = url.pathname.slice(idx + marker.length).replace(/^\/+/, '')
  if (!path) throw new Error('embed source path empty')
  return { origin: url.origin, path }
}

function looksLikeHost(source) {
  const s = String(source || '').trim().toLowerCase()
  return s.includes('.') && !s.includes('/') && !s.includes(' ')
}

function apiRoot(base) {
  const raw = String(base || '').replace(/\/$/, '')
  try {
    const u = new URL(raw)
    // Hostname may be api.ppv.st — only treat path /api as already rooted.
    if (u.pathname === '/api' || u.pathname.startsWith('/api/')) return `${u.origin}/api`
    return `${u.origin}/api`
  } catch {
    return raw.endsWith('/api') ? raw : `${raw}/api`
  }
}

async function lookupIframe(uri) {
  const bases = []
  bases.push(apiRoot(API_BASE))
  for (const d of ['ppv.st', 'ppv.tj', 'ppvs.pk', 'ppv.rw', 'ppv.ms']) {
    const b = `https://api.${d}/api`
    if (!bases.includes(b)) bases.push(b)
  }
  let lastErr = 'api lookup failed'
  for (const base of bases) {
    try {
      const res = await nativeFetch(`${base}/streams/${uri}`, {
        headers: { Accept: 'application/json', 'User-Agent': UA },
      })
      if (!res.ok) {
        lastErr = `upstream ${res.status}`
        continue
      }
      const json = await res.json()
      if (!json?.success || !json?.data) {
        lastErr = json?.error || 'empty stream payload'
        continue
      }
      const source = (json.data.sources || []).find((s) => s.default) || (json.data.sources || [])[0]
      if (!source?.data) {
        lastErr = 'no embed source in api response'
        continue
      }
      return String(source.data)
    } catch (err) {
      lastErr = err instanceof Error ? err.message : String(err)
    }
  }
  throw new Error(lastErr)
}

/**
 * Normalize play params into an embed {origin,path}.
 * New ppv.st shape: source=embed-host, id=embed-path (may contain slashes).
 * Also accepts full iframe URL via id when it starts with https://.
 */
export async function resolveEmbedTarget(source, id, stream = '1') {
  const src = String(source || '').trim()
  const rawId = String(id || '').trim()
  if (!rawId) throw new Error('id required')

  if (/^https?:\/\//i.test(rawId)) {
    return embedFromIframe(rawId)
  }
  if (/^https?:\/\//i.test(src)) {
    return embedFromIframe(src.includes('/embed/') ? src : `${src.replace(/\/$/, '')}/embed/${rawId}`)
  }
  if (looksLikeHost(src)) {
    return { origin: `https://${src.replace(/^https?:\/\//i, '')}`, path: rawId.replace(/^\/+/, '') }
  }
  // source=ppv (or legacy) → resolve iframe from catalog by uri=id
  const iframe = await lookupIframe(rawId)
  return embedFromIframe(iframe)
}

async function postEmbedFetch(embed) {
  const res = await nativeFetch(`${embed.origin}/fetch`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/octet-stream',
      Origin: embed.origin,
      Referer: `${embed.origin}/embed/${embed.path}`,
      'User-Agent': UA,
    },
    body: encodeFetchBody(embed.path),
  })
  if (!res.ok) throw new Error(`embed fetch ${res.status}`)
  const island = res.headers.get('island')
  if (!island) throw new Error('missing island header')
  return { island, body: Buffer.from(await res.arrayBuffer()) }
}

function readVarint(buf, offset) {
  let value = 0
  let shift = 0
  let i = offset
  while (i < buf.length) {
    const byte = buf[i++]
    value |= (byte & 0x7f) << shift
    if (!(byte & 0x80)) return { value, next: i }
    shift += 7
  }
  return { value, next: i }
}

function slugFromFetchBody(body) {
  const buf = Buffer.from(body)
  let i = 0
  while (i < buf.length) {
    const tag = buf[i++]
    const field = tag >> 3
    const wire = tag & 7
    if (wire !== 2) break
    const { value: len, next } = readVarint(buf, i)
    i = next
    if (i + len > buf.length) break
    const value = buf.subarray(i, i + len).toString('utf8')
    i += len
    if (field === 2 && value && !value.startsWith('{')) return value
  }
  return null
}

function extractUrl(memory, slug) {
  const buf = Buffer.from(memory.buffer)
  const text = buf.toString('latin1')
  const re = /https:\/\/[a-z0-9.-]+\/secure\/[^\x00-\x1f\s"']+?index\.m3u8/g
  const matches = []
  let match = null
  while ((match = re.exec(text)) !== null) matches.push(match[0])
  if (slug) return matches.find((url) => url.includes(`/${slug}/`)) || null
  return matches.at(-1) || null
}

async function decryptStreamUrl(island, body, embed, slug) {
  const saved = {
    fetch: globalThis.fetch,
    Request: globalThis.Request,
    Response: globalThis.Response,
    window: globalThis.window,
    Window: globalThis.Window,
    document: globalThis.document,
    location: globalThis.location,
    self: globalThis.self,
    jwplayer: globalThis.jwplayer,
  }

  const jwEngine = { destroy() {} }
  const jwPlayer = {
    remove() {
      return jwEngine
    },
    setup() {},
    on() {},
    load() {},
    getPlaylistItem: () => ({}),
    getState: () => 'idle',
  }
  Object.defineProperty(jwPlayer, '__wasm_jw_player', { value: jwPlayer, enumerable: false })
  Object.defineProperty(jwEngine, '__wasm_jw_engine', { value: jwEngine, enumerable: false })

  const window = new Window({
    url: `${embed.origin}/embed/${embed.path}`,
    settings: { disableJavaScriptFileLoading: true, disableJavaScriptEvaluation: false },
  })

  window.eval = () => undefined
  window.jwplayer = () => jwPlayer
  window.__wasm_jw_player = jwPlayer
  window.__wasm_jw_engine = jwEngine
  window.__wasm_player = { core: { mediaControl: { volume: 0 } } }
  window.__wasm_p2p_config = {}
  window.P2PEngineHls = class {}

  const resolveEmbedUrl = (url) => (typeof url === 'string' && url.startsWith('/') ? `${embed.origin}${url}` : url)

  const embedFetch = async () =>
    new window.Response(body, {
      status: 200,
      headers: { 'content-type': 'application/octet-stream', island },
    })

  const BaseRequest = saved.Request
  globalThis.Request = class extends BaseRequest {
    constructor(input, init) {
      if (typeof input === 'string') {
        super(resolveEmbedUrl(input), init)
        return
      }
      super(input, init)
    }
  }

  globalThis.fetch = embedFetch
  window.fetch = embedFetch
  window.Request = globalThis.Request
  globalThis.Response = window.Response

  Object.assign(globalThis, {
    window,
    Window,
    document: window.document,
    location: window.location,
    self: window,
    jwplayer: window.jwplayer,
  })

  try {
    const mod = await loadWasmModule()
    const wasm = await mod.default({ module_or_path: fs.readFileSync(WASM_BIN) })
    try {
      await wasm.init_wasm?.()
    } catch {
      /* optional on some builds */
    }

    // Runtime flags used by current gasm builds (same offsets as upstream resolvers).
    const u8 = new Uint8Array(wasm.memory.buffer)
    const dv = new DataView(wasm.memory.buffer)
    if (u8.length > 1070513) {
      u8[1070512] = 3
      u8[1070513] = 1
      u8[1070488] = 1
      u8[1070508] = 1
      dv.setInt32(1070476, -2147483648, true)
      dv.setInt32(1070472, 0, true)
      dv.setInt32(1070496, -2147483648, true)
      dv.setInt32(1070492, 0, true)
    }

    // Current ppv embeds expose set_stream(path|island, body). Older builds used set_stream_jw(island, body).
    const payload = new Uint8Array(body)
    const attempts = []
    if (typeof wasm.set_stream === 'function') {
      attempts.push(() => wasm.set_stream(island, payload))
      attempts.push(() => wasm.set_stream(embed.path, payload))
    }
    if (typeof wasm.set_stream_jw === 'function') {
      attempts.push(() => wasm.set_stream_jw(island, payload))
      attempts.push(() => wasm.set_stream_jw(embed.path, payload))
    }
    let streamUrl = null
    for (const attempt of attempts) {
      try {
        const result = attempt()
        await Promise.race([
          Promise.resolve(result),
          new Promise((_, reject) => setTimeout(() => reject(new Error('timeout')), 15_000)),
        ]).catch(() => {})
      } catch {
        /* try next signature */
      }
      streamUrl = extractUrl(wasm.memory, slug)
      if (streamUrl) break
    }
    if (!streamUrl) throw new Error('decrypt did not produce m3u8 url')
    return streamUrl
  } finally {
    globalThis.fetch = saved.fetch
    globalThis.Request = saved.Request
    globalThis.Response = saved.Response
    globalThis.window = saved.window
    globalThis.Window = saved.Window
    globalThis.document = saved.document
    globalThis.location = saved.location
    globalThis.self = saved.self
    globalThis.jwplayer = saved.jwplayer
  }
}

export async function resolveM3U8(source, id, stream = '1') {
  const embed = await resolveEmbedTarget(source, id, stream)
  const { island, body } = await postEmbedFetch(embed)
  const slug = slugFromFetchBody(body)
  const m3u8 = await decryptStreamUrl(island, body, embed, slug)
  return {
    m3u8,
    referer: `${embed.origin}/`,
    embedOrigin: embed.origin,
    embedPath: embed.path,
  }
}

const isCli = (() => {
  try {
    if (!process.argv[1]) return false
    return import.meta.url === pathToFileURL(absPath(process.argv[1])).href
  } catch {
    return false
  }
})()

if (isCli) {
  const args = process.argv.slice(2).filter((a) => a !== '--json')
  const jsonOnly = process.argv.includes('--json') || process.env.STREAMED_RESOLVE_JSON === '1'
  const [source, id, stream = '1'] = args
  if (!source || !id) {
    console.error('usage: resolve.mjs [--json] <source> <id> [stream]')
    process.exit(2)
  }
  try {
    const out = await resolveM3U8(source, id, stream)
    if (jsonOnly) {
      console.log(JSON.stringify(out))
      process.exit(0)
    }
    const r = await nativeFetch(out.m3u8, {
      headers: {
        Referer: out.referer,
        Origin: out.embedOrigin,
        'User-Agent': UA,
      },
    })
    const text = await r.text()
    const lines = text.split('\n').filter((l) => l.length)
    console.log(
      JSON.stringify({
        ...out,
        status: r.status,
        lines: lines.length,
        head: lines.slice(0, 10),
      }),
    )
  } catch (err) {
    if (jsonOnly) {
      console.log(JSON.stringify({ error: err instanceof Error ? err.message : String(err) }))
    } else {
      console.error(err)
    }
    process.exit(1)
  }
}
