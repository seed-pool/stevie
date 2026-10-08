const TECH_INFO_KEY = 'stevie.player.showTechInfo'

/** Stream tech readout (buffer, resolution, bitrate). Default: on. */
export function getShowPlayerTechInfo(): boolean {
  try {
    const v = localStorage.getItem(TECH_INFO_KEY)
    if (v === '0' || v === 'false') return false
    if (v === '1' || v === 'true') return true
  } catch {
    /* ignore */
  }
  return true
}

export function setShowPlayerTechInfo(on: boolean) {
  try {
    localStorage.setItem(TECH_INFO_KEY, on ? '1' : '0')
  } catch {
    /* ignore */
  }
  try {
    window.dispatchEvent(new Event('stevie:player-prefs'))
  } catch {
    /* ignore */
  }
}

/** Exact pixel size, e.g. `1920×1080`. */
export function formatStreamResolution(width: number, height: number): string {
  if (width <= 0 || height <= 0) return ''
  return `${width}×${height}`
}

export function formatStreamBitrate(bps: number): string {
  if (!Number.isFinite(bps) || bps <= 0) return ''
  if (bps >= 1_000_000) {
    const mbps = bps / 1_000_000
    return `${mbps >= 10 ? mbps.toFixed(1) : mbps.toFixed(2)} Mbps`
  }
  if (bps >= 1_000) return `${Math.round(bps / 1_000)} kbps`
  return `${Math.round(bps)} bps`
}

export function formatStreamFps(fps: number): string {
  if (!Number.isFinite(fps) || fps <= 0) return ''
  const rounded = Math.round(fps * 100) / 100
  return Number.isInteger(rounded) ? `${rounded} fps` : `${rounded.toFixed(2)} fps`
}

/** Short codec label from HLS / MIME-ish strings (avc1.42E01E → H.264). */
export function formatStreamCodec(codec?: string | null): string {
  if (!codec) return ''
  const c = codec.toLowerCase()
  if (c.includes('hvc1') || c.includes('hev1') || c.includes('hevc')) return 'HEVC'
  if (c.includes('av01') || c.includes('av1')) return 'AV1'
  if (c.includes('vp09') || c.includes('vp9')) return 'VP9'
  if (c.includes('avc1') || c.includes('avc3') || c.includes('h264')) return 'H.264'
  if (c.includes('mp4a') || c.includes('aac')) return 'AAC'
  // Keep first token if unknown but short.
  const token = codec.split(/[,.\s]/)[0]
  return token && token.length <= 8 ? token.toUpperCase() : ''
}

type HtmlVideoStats = HTMLVideoElement & {
  webkitVideoDecodedByteCount?: number
  getVideoPlaybackQuality?: () => { totalVideoFrames: number; droppedVideoFrames: number }
  requestVideoFrameCallback?: (cb: (now: number) => void) => number
}

export type HtmlVideoTechSample = {
  t: number
  frames: number
  bytes: number
  /** Wall-clock samples for requestVideoFrameCallback fps. */
  rvfcFrames: number
  rvfcT: number
}

/** Sample HTML5 video element stats (progressive remux / direct play). */
export function sampleHtmlVideoTech(
  video: HTMLVideoElement,
  prev?: HtmlVideoTechSample,
  opts?: { downloadedBytes?: number },
): {
  width: number
  height: number
  fps: number
  bps: number
  sample: HtmlVideoTechSample
} {
  const v = video as HtmlVideoStats
  const now = performance.now()
  const q = typeof v.getVideoPlaybackQuality === 'function' ? v.getVideoPlaybackQuality() : null
  const frames = q?.totalVideoFrames ?? 0
  const decodedBytes = typeof v.webkitVideoDecodedByteCount === 'number' ? v.webkitVideoDecodedByteCount : 0
  const downloaded = opts?.downloadedBytes ?? 0
  // Prefer fetch/MSE download counter (works in Firefox); else WebKit decoded bytes.
  const bytes = downloaded > 0 ? downloaded : decodedBytes

  let fps = 0
  let bps = 0
  if (prev && prev.t > 0) {
    const dt = (now - prev.t) / 1000
    if (dt >= 0.25) {
      if (frames >= prev.frames) fps = (frames - prev.frames) / dt
      if (bytes >= prev.bytes && bytes > 0) bps = ((bytes - prev.bytes) * 8) / dt
    }
  }
  // Average bitrate from total downloaded bytes / media time (stable for VOD remux).
  if (!bps && downloaded > 0 && video.currentTime >= 0.75) {
    bps = (downloaded * 8) / video.currentTime
  }

  return {
    width: video.videoWidth || 0,
    height: video.videoHeight || 0,
    fps,
    bps,
    sample: {
      t: now,
      frames,
      bytes,
      rvfcFrames: prev?.rvfcFrames ?? 0,
      rvfcT: prev?.rvfcT ?? 0,
    },
  }
}

/** Attach requestVideoFrameCallback loop; returns cancel function. */
export function attachVideoFrameFps(
  video: HTMLVideoElement,
  onFps: (fps: number) => void,
): () => void {
  const v = video as HtmlVideoStats
  if (typeof v.requestVideoFrameCallback !== 'function') return () => undefined
  let cancelled = false
  let frames = 0
  let windowStart = 0
  let handle = 0
  const tick = (now: number) => {
    if (cancelled) return
    if (!windowStart) windowStart = now
    frames++
    const elapsed = now - windowStart
    if (elapsed >= 1000) {
      onFps((frames * 1000) / elapsed)
      frames = 0
      windowStart = now
    }
    handle = v.requestVideoFrameCallback!(tick)
  }
  handle = v.requestVideoFrameCallback(tick)
  return () => {
    cancelled = true
    try {
      const cancel = (v as { cancelVideoFrameCallback?: (h: number) => void }).cancelVideoFrameCallback
      cancel?.(handle)
    } catch {
      /* ignore */
    }
  }
}
