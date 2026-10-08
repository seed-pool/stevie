export type ClientCaps = {
  hevc: boolean
  av1: boolean
  vp9: boolean
  aac: boolean
  mp3: boolean
  ac3: boolean
  eac3: boolean
  opus: boolean
  flac: boolean
}

function canPlay(type: string) {
  if (typeof document === 'undefined') return false
  const v = document.createElement('video')
  const r = v.canPlayType(type)
  return r === 'probably' || r === 'maybe'
}

export function detectClientCaps(): ClientCaps {
  return {
    hevc: canPlay('video/mp4; codecs="hvc1.1.6.L93.B0"') || canPlay('video/mp4; codecs="hev1.1.6.L93.B0"'),
    av1: canPlay('video/mp4; codecs="av01.0.05M.08"'),
    vp9: canPlay('video/webm; codecs="vp9"'),
    aac: canPlay('audio/mp4; codecs="mp4a.40.2"'),
    mp3: canPlay('audio/mpeg'),
    ac3: canPlay('audio/mp4; codecs="ac-3"'),
    eac3: canPlay('audio/mp4; codecs="ec-3"'),
    opus: canPlay('audio/webm; codecs="opus"'),
    flac: canPlay('audio/mp4; codecs="flac"'),
  }
}

export function capsQuery(caps: ClientCaps): string {
  return Object.entries(caps)
    .map(([k, v]) => `${k}=${v ? '1' : '0'}`)
    .join('&')
}
