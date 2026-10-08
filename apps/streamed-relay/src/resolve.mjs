import { readFileSync } from 'node:fs'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { Window } from 'happy-dom'

const embedOrigin = (process.env.STEVIE_STREAMED_EMBED_URL || 'https://embed.st').replace(/\/$/, '')
const wasmBytes = readFileSync(new URL('../vendor/lock.wasm', import.meta.url))
const nativeFetch = globalThis.fetch.bind(globalThis)

function varint(n) {
  const bytes = []
  let v = n >>> 0
  while (v > 0x7f) {
    bytes.push((v & 0x7f) | 0x80)
    v >>>= 7
  }
  bytes.push(v)
  return Uint8Array.from(bytes)
}
function fieldStr(field, value) {
  const body = new TextEncoder().encode(value)
  const tag = Uint8Array.of((field << 3) | 2)
  const len = varint(body.length)
  const out = new Uint8Array(tag.length + len.length + body.length)
  out.set(tag, 0)
  out.set(len, tag.length)
  out.set(body, tag.length + len.length)
  return out
}
function encodeFetchBody(source, id, stream) {
  const parts = [fieldStr(1, source), fieldStr(2, id), fieldStr(3, String(stream))]
  const total = parts.reduce((n, p) => n + p.length, 0)
  const out = new Uint8Array(total)
  let o = 0
  for (const part of parts) {
    out.set(part, o)
    o += part.length
  }
  return out
}
function asBody(data) {
  return new Uint8Array(data.buffer, data.byteOffset, data.byteLength)
}

async function postFetch(source, id, stream) {
  const path = `${source}/${id}/${stream}`
  const res = await nativeFetch(`${embedOrigin}/fetch`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/octet-stream',
      Origin: embedOrigin,
      Referer: `${embedOrigin}/embed/${path}`,
      'User-Agent':
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
    },
    body: encodeFetchBody(source, id, stream),
  })
  if (!res.ok) throw new Error(`embed /fetch ${res.status}`)
  const goat = res.headers.get('goat')
  if (!goat) throw new Error('missing goat header')
  return { body: Buffer.from(await res.arrayBuffer()), goat, path }
}

function mountDom(path) {
  const window = new Window({ url: `${embedOrigin}/embed/${path}` })
  const doc = window.document
  doc.body.innerHTML = '<div id="player"></div>'
  const jwCfg = { file: null }
  const jwBase = {
    getContainer: () => doc.getElementById('player'),
    getState: () => 'idle',
    load: (cfg) => {
      if (cfg?.file) jwCfg.file = cfg.file
    },
    setConfig: (cfg) => {
      if (cfg?.file) jwCfg.file = cfg.file
    },
    getConfig: () => jwCfg,
    setup() {},
    on() {},
    play() {},
    getPlaylistItem: () => jwCfg,
    getPlaylist: () => (jwCfg.file ? [{ file: jwCfg.file }] : []),
  }
  const proxy = new Proxy(jwBase, {
    get(target, prop, receiver) {
      if (Reflect.has(target, prop)) return Reflect.get(target, prop, receiver)
      if (prop === Symbol.toStringTag) return 'Object'
      return () => null
    },
  })
  Object.assign(window, { __wasm_jw_player: proxy, jwplayer: () => proxy })
  Object.assign(globalThis, {
    window,
    document: doc,
    location: window.location,
    self: window,
    atob: (s) => Buffer.from(s, 'base64').toString('binary'),
    btoa: (s) => Buffer.from(s, 'binary').toString('base64'),
  })
  const NativeRequest = globalThis.Request
  const NativeUrl = globalThis.URL
  Object.defineProperty(globalThis, 'URL', {
    configurable: true,
    writable: true,
    value: class extends NativeUrl {
      constructor(input, base) {
        super(input === '/fetch' ? `${embedOrigin}/fetch` : input, base ?? `${embedOrigin}/`)
      }
    },
  })
  Object.defineProperty(globalThis, 'Request', {
    configurable: true,
    writable: true,
    value: class extends NativeRequest {
      constructor(input, init) {
        super(input === '/fetch' ? `${embedOrigin}/fetch` : input, init)
      }
    },
  })
  Object.assign(window, {
    URL: globalThis.URL,
    Request: globalThis.Request,
    Response: globalThis.Response,
    Headers: globalThis.Headers,
  })
  return jwCfg
}

function mockFetch(goat, body, onM3u8) {
  return async (input) => {
    const href =
      typeof input === 'string' ? input : input instanceof URL ? input.href : String(input.url)
    if (href.includes('lock.wasm')) {
      return new Response(asBody(wasmBytes), {
        status: 200,
        headers: { 'Content-Type': 'application/wasm' },
      })
    }
    if (href.includes('/fetch')) {
      return new Response(asBody(body), {
        status: 200,
        headers: { goat, 'Content-Type': 'application/octet-stream' },
      })
    }
    if (href.includes('.m3u8')) {
      onM3u8(href)
      return new Response('#EXTM3U\n#EXT-X-VERSION:3\n', {
        status: 200,
        headers: { 'Content-Type': 'application/vnd.apple.mpegurl' },
      })
    }
    return new Response('', { status: 404 })
  }
}

function patchImports(imports, goat, body, onM3u8) {
  const bg = imports?.['./locked_bg.js']
  if (!bg) return
  for (const key of Object.keys(bg)) {
    if (!key.includes('instanceof')) continue
    const orig = bg[key]
    if (typeof orig !== 'function') continue
    bg[key] = (...args) => (orig(...args) ? 1 : 1)
  }
  const fetchKey = Object.keys(bg).find((k) => k.includes('fetch_e6e8e0'))
  if (!fetchKey || typeof bg[fetchKey] !== 'function') return
  bg[fetchKey] = (_win, req) => {
    const href = req?.url ?? ''
    if (href.includes('/fetch')) {
      return Promise.resolve(
        new Response(asBody(body), {
          status: 200,
          headers: { goat, 'Content-Type': 'application/octet-stream' },
        }),
      )
    }
    if (href.includes('.m3u8')) {
      onM3u8(href)
      return Promise.resolve(
        new Response('#EXTM3U\n#EXT-X-VERSION:3\n', {
          status: 200,
          headers: { 'Content-Type': 'application/vnd.apple.mpegurl' },
        }),
      )
    }
    return Promise.reject(new Error(`unexpected wasm fetch ${href}`))
  }
}

export async function resolveM3U8(source, id, stream = '1') {
  const { body, goat, path } = await postFetch(source, id, stream)
  let m3u8 = null
  const onM3u8 = (url) => {
    m3u8 = url
  }
  const jwCfg = mountDom(path)
  const fetchFn = mockFetch(goat, body, onM3u8)
  globalThis.fetch = fetchFn

  const wasm = globalThis.WebAssembly
  const origInstantiate = wasm.instantiate.bind(wasm)
  wasm.instantiate = async (sourceObj, imports) => {
    patchImports(imports, goat, body, onM3u8)
    let bytes = sourceObj
    if (!(sourceObj instanceof ArrayBuffer) && !ArrayBuffer.isView(sourceObj)) {
      bytes = wasmBytes.buffer.slice(wasmBytes.byteOffset, wasmBytes.byteOffset + wasmBytes.byteLength)
    }
    return origInstantiate(bytes, imports)
  }
  wasm.instantiateStreaming = async (_resp, imports) => wasm.instantiate(wasmBytes, imports)

  try {
    const mod = await import(pathToFileURL(fileURLToPath(new URL('../vendor/lock.js', import.meta.url))).href)
    const factory = mod.default || mod
    const api = await factory({
      module_or_path: `${embedOrigin}/js/wasm/lock.wasm`,
      fetch: fetchFn,
    })
    await api.init_wasm?.()
    try {
      await api.set_stream_jw(source, id, String(stream))
    } catch (err) {
      if (!m3u8 && !jwCfg.file) throw err
    }
  } finally {
    wasm.instantiate = origInstantiate
    delete wasm.instantiateStreaming
    globalThis.fetch = nativeFetch
  }
  const url = m3u8 || jwCfg.file
  if (!url) throw new Error('lock did not yield m3u8')
  return { m3u8: url, referer: `${embedOrigin}/` }
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
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
        Origin: embedOrigin,
        'User-Agent':
          'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
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
