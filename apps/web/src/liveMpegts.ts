import type { Config } from 'mpegts.js'

/** Stable IPTV tuning: prefer continuity over low latency. */
export const LIVE_MPEGTS_CONFIG: Config = {
  enableWorker: false,
  enableWorkerForMSE: false,
  enableStashBuffer: true,
  // Default is 384KB; 128KB was too small and caused stutter on jittery feeds.
  stashInitialSize: 1024 * 1024,
  isLive: true,
  // Latency chasing seeks/skips — that feels like staggering on unstable links.
  liveBufferLatencyChasing: false,
  liveBufferLatencyChasingOnPaused: false,
  liveSync: false,
  // Keep the HTTP connection open; lazyLoad aborts and causes live stalls.
  lazyLoad: false,
  deferLoadAfterSourceOpen: false,
  autoCleanupSourceBuffer: true,
  autoCleanupMaxBackwardDuration: 90,
  autoCleanupMinBackwardDuration: 30,
  fixAudioTimestampGap: true,
}

/** Seconds of media we want buffered before starting / after a stall. */
export const LIVE_MIN_BUFFER_SEC = 3
export const LIVE_REBUFFER_SEC = 2

export function bufferedAhead(video: HTMLMediaElement): number {
  const { buffered, currentTime } = video
  if (!buffered.length) return 0
  // Find the range that contains or is ahead of currentTime.
  for (let i = 0; i < buffered.length; i++) {
    const start = buffered.start(i)
    const end = buffered.end(i)
    if (currentTime >= start - 0.5 && currentTime <= end) {
      return Math.max(0, end - currentTime)
    }
  }
  // Otherwise take the end of the last range.
  const end = buffered.end(buffered.length - 1)
  return Math.max(0, end - currentTime)
}

export function canPlayDirectUrl(streamUrl: string): boolean {
  if (!streamUrl) return false
  if (streamUrl.startsWith('https:')) return true
  if (streamUrl.startsWith('http:')) return window.location.protocol === 'http:'
  return true
}

/** Xtream panels usually allow HLS; web players should prefer .m3u8 over .ts. */
export function preferHlsUrl(streamUrl: string): string {
  const u = streamUrl.trim()
  if (!u) return u
  // Same-origin Stevie proxy already serves HLS.
  if (/\/api\/live\/channels\/[^/]+\/stream(?:\?|$)/i.test(u)) return u
  if (/\/live\/.+\.ts$/i.test(u)) return u.replace(/\.ts$/i, '.m3u8')
  return u
}

/** Wait until we have enough buffered media (or timeout). */
export function waitForBuffer(
  video: HTMLMediaElement,
  minSeconds: number,
  timeoutMs = 20_000,
  signal?: AbortSignal,
): Promise<boolean> {
  return new Promise((resolve) => {
    if (bufferedAhead(video) >= minSeconds) {
      resolve(true)
      return
    }
    const started = Date.now()
    const onProgress = () => {
      if (signal?.aborted) {
        cleanup()
        resolve(false)
        return
      }
      if (bufferedAhead(video) >= minSeconds) {
        cleanup()
        resolve(true)
        return
      }
      if (Date.now() - started >= timeoutMs) {
        cleanup()
        resolve(bufferedAhead(video) > 0.5)
      }
    }
    const cleanup = () => {
      video.removeEventListener('progress', onProgress)
      video.removeEventListener('loadeddata', onProgress)
      video.removeEventListener('canplay', onProgress)
      window.clearInterval(timer)
    }
    video.addEventListener('progress', onProgress)
    video.addEventListener('loadeddata', onProgress)
    video.addEventListener('canplay', onProgress)
    const timer = window.setInterval(onProgress, 250)
    signal?.addEventListener('abort', () => {
      cleanup()
      resolve(false)
    })
  })
}
