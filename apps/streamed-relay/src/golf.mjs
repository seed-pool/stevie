/**
 * Golf sources do not unlock via lock.wasm directly.
 * embed.st/embed/golf/... → rockystream iframe → base64 ingest URL →
 * embed.st/embed/ingest/{id}/{stream} (then normal GOAT unlock).
 */

const UA =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36'

function headers(referer) {
  return {
    'User-Agent': UA,
    Accept: 'text/html,application/xhtml+xml',
    Referer: referer,
  }
}

function iframeSrc(html) {
  const m = html.match(/iframe\s+src="([^"]+)"/i)
  if (!m?.[1]) throw new Error('golf embed iframe missing')
  return m[1].replace(/&amp;/g, '&')
}

/** Prefer rockystream data-source=… or inline atob("…") ingest URLs. */
function ingestFromRocky(html) {
  const fromAttr = html.match(/data-source="([^"]+)"/i)
  if (fromAttr?.[1]) {
    return decodeIngestUrl(fromAttr[1])
  }
  const fromAtob = html.match(/atob\(\s*["']([A-Za-z0-9+/=]+)["']\s*\)/)
  if (fromAtob?.[1]) {
    return decodeIngestUrl(fromAtob[1])
  }
  throw new Error('rockystream ingest target missing (data-source / atob)')
}

function decodeIngestUrl(b64) {
  let decoded
  try {
    decoded = Buffer.from(b64, 'base64').toString('utf8')
  } catch {
    throw new Error('rockystream ingest payload not base64')
  }
  let u
  try {
    u = new URL(decoded)
  } catch {
    throw new Error(`unexpected ingest url ${decoded}`)
  }
  const path = u.pathname.replace(/^\/+|\/+$/g, '').split('/')
  // embed / ingest / {id} / {stream}
  if (path[0] !== 'embed' || path[1] !== 'ingest' || !path[2] || !path[3]) {
    throw new Error(`unexpected ingest path ${decoded}`)
  }
  return { source: 'ingest', id: path[2], stream: path[3] }
}

/**
 * Map a golf slot to the underlying ingest GOAT slot.
 * @returns {Promise<{ source: string, id: string, stream: string }>}
 */
export async function mapGolfToIngest(source, id, stream = '1', embedOrigin = 'https://embed.st') {
  const origin = embedOrigin.replace(/\/$/, '')
  const src = String(source || '').trim().toLowerCase()
  if (src !== 'golf') {
    return { source: String(source), id: String(id), stream: String(stream) }
  }
  const embedUrl = `${origin}/embed/golf/${id}/${stream}`
  const embedRes = await fetch(embedUrl, { headers: headers(`${origin}/`) })
  if (!embedRes.ok) throw new Error(`golf embed HTTP ${embedRes.status}`)
  const embedHtml = await embedRes.text()
  const rockyUrl = iframeSrc(embedHtml)
  const rockyRes = await fetch(rockyUrl, { headers: headers(embedUrl) })
  if (!rockyRes.ok) throw new Error(`rockystream HTTP ${rockyRes.status}`)
  const rockyHtml = await rockyRes.text()
  return ingestFromRocky(rockyHtml)
}
