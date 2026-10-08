/** Fetch progressive fMP4 into MSE while counting downloaded bytes (bitrate meter). */

export type Fmp4FetchHandlers = {
  onBytes?: (totalBytes: number) => void
  onError?: (message: string) => void
}

function mimeFromHint(videoCodecHint?: string): string[] {
  const c = (videoCodecHint || '').toUpperCase()
  const list: string[] = []
  if (c.includes('HEVC') || c.includes('H.265') || c.includes('H265')) {
    list.push('video/mp4; codecs="hvc1.1.6.L90.B0,mp4a.40.2"', 'video/mp4; codecs="hev1.1.6.L90.B0,mp4a.40.2"')
  }
  if (c.includes('AV1')) list.push('video/mp4; codecs="av01.0.05M.08,mp4a.40.2"')
  list.push(
    'video/mp4; codecs="avc1.640028,mp4a.40.2"',
    'video/mp4; codecs="avc1.4D401F,mp4a.40.2"',
    'video/mp4; codecs="avc1.42E01E,mp4a.40.2"',
    'video/mp4; codecs="avc1.64001F,mp4a.40.2"',
  )
  return list.filter((m, i, a) => a.indexOf(m) === i && MediaSource.isTypeSupported(m))
}

function hex2(n: number) {
  return n.toString(16).padStart(2, '0').toUpperCase()
}

/** Pull AVC codec string from an avcC box when present. */
function avcCodecFromBuf(buf: Uint8Array): string | null {
  const n = Math.min(buf.length, 512 * 1024)
  for (let i = 0; i + 8 < n; i++) {
    if (buf[i + 4] === 0x61 && buf[i + 5] === 0x76 && buf[i + 6] === 0x63 && buf[i + 7] === 0x43) {
      // avcC: after size(4)+'avcC'(4) → configurationVersion, AVCProfileIndication, profile_compatibility, AVCLevelIndication
      const base = i + 8
      if (base + 4 >= n) return null
      const profile = buf[base + 1]!
      const compat = buf[base + 2]!
      const level = buf[base + 3]!
      return `avc1.${hex2(profile)}${hex2(compat)}${hex2(level)}`
    }
  }
  return null
}

/** Best-effort codec sniff from early fMP4 bytes (ftyp/moov). */
function sniffMimes(buf: Uint8Array): string[] {
  const n = Math.min(buf.length, 256 * 1024)
  let ascii = ''
  for (let i = 0; i < n; i++) ascii += String.fromCharCode(buf[i]!)
  const pick = (mimes: string[]) => mimes.filter((m) => MediaSource.isTypeSupported(m))
  const out: string[] = []
  if (ascii.includes('hvc1') || ascii.includes('hev1') || ascii.includes('hvcC')) {
    out.push(...pick(['video/mp4; codecs="hvc1.1.6.L90.B0,mp4a.40.2"', 'video/mp4; codecs="hev1.1.6.L90.B0,mp4a.40.2"']))
  }
  if (ascii.includes('av01') || ascii.includes('av1C')) {
    out.push(...pick(['video/mp4; codecs="av01.0.05M.08,mp4a.40.2"']))
  }
  if (ascii.includes('avc1') || ascii.includes('avcC')) {
    const exact = avcCodecFromBuf(buf)
    if (exact) {
      const m = `video/mp4; codecs="${exact},mp4a.40.2"`
      if (MediaSource.isTypeSupported(m)) out.push(m)
    }
    out.push(
      ...pick([
        'video/mp4; codecs="avc1.640028,mp4a.40.2"',
        'video/mp4; codecs="avc1.4D401F,mp4a.40.2"',
        'video/mp4; codecs="avc1.42E01E,mp4a.40.2"',
        'video/mp4; codecs="avc1.64001F,mp4a.40.2"',
      ]),
    )
  }
  // Dedupe
  return out.filter((m, i, a) => a.indexOf(m) === i)
}

function concat(chunks: Uint8Array[]): Uint8Array {
  const total = chunks.reduce((n, c) => n + c.byteLength, 0)
  const out = new Uint8Array(total)
  let off = 0
  for (const c of chunks) {
    out.set(c, off)
    off += c.byteLength
  }
  return out
}

/**
 * Play a same-origin fMP4 remux URL via MediaSource, counting bytes for bitrate.
 * Returns a dispose function. On failure calls onError — do NOT fall back to
 * progressive &lt;video src&gt; for empty_moov pipes (Firefox throws NS_ERROR_DOM_MEDIA_RANGE_ERR).
 */
export function attachFmp4FetchPlayback(
  video: HTMLVideoElement,
  url: string,
  opts: Fmp4FetchHandlers & { videoCodecHint?: string; signal?: AbortSignal } = {},
): () => void {
  const ms = new MediaSource()
  const objectUrl = URL.createObjectURL(ms)
  video.src = objectUrl

  let totalBytes = 0
  let sb: SourceBuffer | null = null
  let closed = false
  const queue: Uint8Array[] = []
  let appending = false
  let streamDone = false

  const dispose = () => {
    if (closed) return
    closed = true
    try {
      video.pause()
    } catch {
      /* ignore */
    }
    try {
      if (sb && ms.readyState === 'open') {
        try {
          ms.removeSourceBuffer(sb)
        } catch {
          /* ignore */
        }
      }
      if (ms.readyState === 'open') ms.endOfStream()
    } catch {
      /* ignore */
    }
    try {
      video.removeAttribute('src')
      video.load()
    } catch {
      /* ignore */
    }
    URL.revokeObjectURL(objectUrl)
  }

  const fail = (msg: string) => {
    dispose()
    opts.onError?.(msg)
  }

  const pump = () => {
    if (closed || !sb || appending) return
    if (queue.length === 0) {
      if (streamDone && ms.readyState === 'open') {
        try {
          ms.endOfStream()
        } catch {
          /* ignore */
        }
      }
      return
    }
    const chunk = queue.shift()!
    appending = true
    try {
      const copy = new Uint8Array(chunk.byteLength)
      copy.set(chunk)
      sb.appendBuffer(copy)
    } catch (err) {
      appending = false
      fail(err instanceof Error ? err.message : 'SourceBuffer append failed')
    }
  }

  const openSourceBuffer = (candidates: string[]): SourceBuffer | null => {
    for (const mime of candidates) {
      try {
        const buf = ms.addSourceBuffer(mime)
        buf.mode = 'segments'
        return buf
      } catch {
        /* try next */
      }
    }
    return null
  }

  let startTimer = 0
  ms.addEventListener('sourceopen', () => {
    if (closed) return

    startTimer = window.setTimeout(() => {
      if (!closed && totalBytes === 0) fail('Remux stream timed out — no data received')
    }, 20000)

    ;(async () => {
      try {
        const res = await fetch(url, {
          credentials: 'include',
          signal: opts.signal,
          headers: { Accept: 'video/mp4,*/*' },
        })
        if (!res.ok || !res.body) throw new Error(`remux HTTP ${res.status}`)
        const reader = res.body.getReader()
        const head: Uint8Array[] = []
        let headBytes = 0
        let candidates: string[] = []

        // Buffer until we can sniff codecs (or hit a size cap), then open SourceBuffer.
        while (candidates.length === 0) {
          const { done, value } = await reader.read()
          if (done) break
          if (!value?.length) continue
          totalBytes += value.byteLength
          opts.onBytes?.(totalBytes)
          if (totalBytes > 0) window.clearTimeout(startTimer)
          head.push(value)
          headBytes += value.byteLength
          const joined = concat(head)
          candidates = sniffMimes(joined)
          if (candidates.length === 0 && headBytes >= 512 * 1024) break
          if (candidates.length > 0) {
            head.length = 0
            queue.push(joined)
          }
        }
        if (candidates.length === 0) {
          candidates = mimeFromHint(opts.videoCodecHint)
          if (head.length) queue.push(concat(head))
        }
        if (candidates.length === 0) {
          fail('No supported fMP4 codec string')
          await reader.cancel().catch(() => undefined)
          return
        }

        sb = openSourceBuffer(candidates)
        if (!sb) {
          fail('addSourceBuffer failed for all codec candidates')
          await reader.cancel().catch(() => undefined)
          return
        }
        sb.addEventListener('updateend', () => {
          appending = false
          pump()
        })
        sb.addEventListener('error', () => fail('SourceBuffer error'))
        pump()

        for (;;) {
          if (closed) {
            await reader.cancel().catch(() => undefined)
            break
          }
          const { done, value } = await reader.read()
          if (done) {
            streamDone = true
            pump()
            break
          }
          if (!value?.length) continue
          totalBytes += value.byteLength
          opts.onBytes?.(totalBytes)
          queue.push(value)
          pump()
        }
      } catch (err) {
        if (closed || opts.signal?.aborted) return
        fail(err instanceof Error ? err.message : 'fMP4 fetch failed')
      } finally {
        window.clearTimeout(startTimer)
      }
    })()
  })

  return () => {
    window.clearTimeout(startTimer)
    dispose()
  }
}

export function canUseFmp4FetchPlayback(videoCodecHint?: string): boolean {
  if (typeof MediaSource === 'undefined') return false
  return mimeFromHint(videoCodecHint).length > 0 || MediaSource.isTypeSupported('video/mp4; codecs="avc1.42E01E,mp4a.40.2"')
}

/** Progressive &lt;video src&gt; cannot play empty_moov remux pipes in Firefox. */
export function remuxNeedsMediaSource(): boolean {
  return typeof MediaSource !== 'undefined'
}
