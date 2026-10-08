import http from 'node:http'
import { spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { Impit } from 'impit'

const PORT = Number(process.env.PORT || 8091)
const EMBED = (process.env.STEVIE_STREAMED_EMBED_URL || 'https://embed.st').replace(/\/$/, '')
const UA =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36'
const RESOLVE_SCRIPT = fileURLToPath(new URL('./resolve.mjs', import.meta.url))
const CACHE_TTL_MS = 55_000

const client = new Impit({ browser: 'chrome' })
let warmed = false

/** @type {Map<string, { m3u8: string, referer: string, cachedAt: number }>} */
const resolveCache = new Map()
/** Serialize unlock subprocesses — lock.wasm cannot safely re-enter one Node process. */
let resolveChain = Promise.resolve()

async function warm() {
  if (warmed) return
  try {
    await client.fetch(`${EMBED}/`, { headers: { 'User-Agent': UA } })
    warmed = true
  } catch {
    /* ignore */
  }
}

function json(res, status, body) {
  const data = JSON.stringify(body)
  res.writeHead(status, {
    'Content-Type': 'application/json',
    'Cache-Control': 'no-store',
  })
  res.end(data)
}

function readJSON(req) {
  return new Promise((resolve, reject) => {
    const chunks = []
    req.on('data', (c) => chunks.push(c))
    req.on('end', () => {
      try {
        resolve(JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}'))
      } catch (err) {
        reject(err)
      }
    })
    req.on('error', reject)
  })
}

/** Fresh process per unlock — happy-dom + lock.wasm pollute globalThis. */
function resolveInSubprocess(source, id, stream) {
  return new Promise((resolve, reject) => {
    const child = spawn(
      process.execPath,
      [RESOLVE_SCRIPT, '--json', source, id, String(stream)],
      {
        cwd: fileURLToPath(new URL('..', import.meta.url)),
        env: { ...process.env, STREAMED_RESOLVE_JSON: '1' },
        stdio: ['ignore', 'pipe', 'pipe'],
      },
    )
    let stdout = ''
    let stderr = ''
    child.stdout.on('data', (c) => {
      stdout += c
    })
    child.stderr.on('data', (c) => {
      stderr += c
    })
    const timer = setTimeout(() => {
      child.kill('SIGKILL')
      reject(new Error('resolve timed out'))
    }, 28_000)
    child.on('error', (err) => {
      clearTimeout(timer)
      reject(err)
    })
    child.on('close', (code) => {
      clearTimeout(timer)
      if (code !== 0) {
        reject(new Error(stderr.trim() || stdout.trim() || `resolve exit ${code}`))
        return
      }
      try {
        const line = stdout
          .split('\n')
          .map((l) => l.trim())
          .find((l) => l.startsWith('{'))
        const out = JSON.parse(line || '{}')
        if (!out.m3u8) throw new Error(out.error || 'no m3u8')
        resolve({ m3u8: out.m3u8, referer: out.referer || `${EMBED}/` })
      } catch (err) {
        reject(err instanceof Error ? err : new Error(String(err)))
      }
    })
  })
}

function enqueueResolve(source, id, stream) {
  const key = `${source}|${id}|${stream}`
  const hit = resolveCache.get(key)
  if (hit && Date.now() - hit.cachedAt < CACHE_TTL_MS) {
    return Promise.resolve({ m3u8: hit.m3u8, referer: hit.referer })
  }
  const job = resolveChain.then(() => resolveInSubprocess(source, id, stream))
  resolveChain = job.then(
    () => undefined,
    () => undefined,
  )
  return job.then((out) => {
    resolveCache.set(key, { ...out, cachedAt: Date.now() })
    return out
  })
}

/** Absolutize relative playlist lines so Stevie can re-proxy each URI. */
function absolutizePlaylist(text, baseUrl) {
  return text
    .split('\n')
    .map((line) => {
      const trimmed = line.trim()
      if (!trimmed) return line
      if (trimmed.startsWith('#')) {
        return line.replace(/URI="([^"]+)"/g, (_, u) => `URI="${new URL(u, baseUrl).href}"`)
      }
      return new URL(trimmed, baseUrl).href
    })
    .join('\n')
}

async function proxyHLS(_req, res, url) {
  await warm()
  const upstream = await client.fetch(url, {
    headers: {
      Referer: `${EMBED}/`,
      Origin: EMBED,
      Accept: '*/*',
      'User-Agent': UA,
    },
  })
  const ct = upstream.headers.get('content-type') || ''
  const buf = Buffer.from(await upstream.arrayBuffer())
  if (!upstream.ok) {
    res.writeHead(upstream.status, { 'Content-Type': ct || 'text/plain', 'Cache-Control': 'no-store' })
    res.end(buf)
    return
  }
  const isPlaylist =
    ct.includes('mpegurl') ||
    url.includes('.m3u8') ||
    buf.slice(0, 7).toString() === '#EXTM3U'
  if (isPlaylist) {
    const text = absolutizePlaylist(buf.toString('utf8'), url)
    res.writeHead(200, {
      'Content-Type': 'application/vnd.apple.mpegurl',
      'Cache-Control': 'no-store',
      'Access-Control-Allow-Origin': '*',
    })
    res.end(text)
    return
  }
  res.writeHead(200, {
    'Content-Type': ct || 'video/mp2t',
    'Cache-Control': 'no-store',
    'Access-Control-Allow-Origin': '*',
  })
  res.end(buf)
}

const server = http.createServer(async (req, res) => {
  try {
    const u = new URL(req.url || '/', `http://${req.headers.host || '127.0.0.1'}`)
    if (req.method === 'GET' && u.pathname === '/health') {
      json(res, 200, { ok: true })
      return
    }
    if (req.method === 'POST' && u.pathname === '/resolve') {
      const body = await readJSON(req)
      const source = String(body.source || '').trim()
      const id = String(body.id || '').trim()
      const stream = String(body.stream || body.streamNo || '1')
      if (!source || !id) {
        json(res, 400, { ok: false, error: 'source and id required' })
        return
      }
      const out = await enqueueResolve(source, id, stream)
      json(res, 200, { ok: true, ...out, source, id, stream })
      return
    }
    if (req.method === 'GET' && u.pathname === '/hls') {
      const target = u.searchParams.get('url')
      if (!target || !/^https:\/\//i.test(target)) {
        json(res, 400, { ok: false, error: 'url required' })
        return
      }
      await proxyHLS(req, res, target)
      return
    }
    json(res, 404, { ok: false, error: 'not found' })
  } catch (err) {
    json(res, 500, { ok: false, error: err instanceof Error ? err.message : String(err) })
  }
})

server.listen(PORT, '0.0.0.0', () => {
  console.log(`streamed-relay listening on :${PORT}`)
})
