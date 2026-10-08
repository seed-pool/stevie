import { useCallback, useEffect, useRef, useState } from 'react'
import { api, formatDuration, parseDurationSec } from './api'
import { attachFmp4FetchPlayback, canUseFmp4FetchPlayback, remuxNeedsMediaSource } from './fmp4FetchPlayback'
import { Icon, icons } from './icons'
import { bufferedAhead } from './liveMpegts'
import {
  attachVideoFrameFps,
  formatStreamBitrate,
  formatStreamFps,
  formatStreamResolution,
  getShowPlayerTechInfo,
  sampleHtmlVideoTech,
  type HtmlVideoTechSample,
} from './playerPrefs'
import { usePlayerChrome } from './usePlayerChrome'

type Props = {
  fileName: string
  streamUrl: string
  title?: string
  /** Known duration in seconds (VOD metadata / recording probe). Enables seek. */
  durationSec?: number
  modeLabel?: string
  mode: 'expanded' | 'docked'
  onClose: () => void
  onMinimize: () => void
  onExpand: () => void
}

function stopVideoElement(video: HTMLVideoElement | null) {
  if (!video) return
  try {
    video.pause()
    video.removeAttribute('src')
    video.load()
  } catch {
    /* ignore */
  }
}

export function RecordingPlayer({
  fileName,
  streamUrl,
  title,
  durationSec,
  modeLabel = 'Recording',
  mode,
  onClose,
  onMinimize,
  onExpand,
}: Props) {
  const rootRef = useRef<HTMLDivElement>(null)
  const videoRef = useRef<HTMLVideoElement>(null)
  const remuxStartRef = useRef(0)
  const durationRef = useRef(0)
  const [error, setError] = useState('')
  const [playing, setPlaying] = useState(false)
  const [muted, setMuted] = useState(false)
  const [current, setCurrent] = useState(0)
  const [duration, setDuration] = useState(() => (durationSec && durationSec > 0 ? durationSec : 0))
  const [scrub, setScrub] = useState<number | null>(null)
  const [fullscreen, setFullscreen] = useState(false)
  const [reloadToken, setReloadToken] = useState(0)
  const [showTechInfo, setShowTechInfo] = useState(getShowPlayerTechInfo)
  const [resolution, setResolution] = useState('')
  const [bitrateLabel, setBitrateLabel] = useState('')
  const [fpsLabel, setFpsLabel] = useState('')
  const [bufferSec, setBufferSec] = useState(0)
  const techSampleRef = useRef<HtmlVideoTechSample>({ t: 0, frames: 0, bytes: 0, rvfcFrames: 0, rvfcT: 0 })
  const downloadedBytesRef = useRef(0)
  const metaBitrateBpsRef = useRef(0)
  const videoCodecHintRef = useRef('')
  const docked = mode === 'docked'

  const chromePinned = !!(error || !playing || scrub != null)
  const { chromeOn, reveal, hold, scheduleHide } = usePlayerChrome({ pinned: chromePinned, docked })

  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === 'stevie.player.showTechInfo') setShowTechInfo(getShowPlayerTechInfo())
    }
    const onPref = () => setShowTechInfo(getShowPlayerTechInfo())
    window.addEventListener('storage', onStorage)
    window.addEventListener('stevie:player-prefs', onPref)
    return () => {
      window.removeEventListener('storage', onStorage)
      window.removeEventListener('stevie:player-prefs', onPref)
    }
  }, [])

  const syncVideoStats = useCallback(() => {
    const video = videoRef.current
    if (!video) return
    const { width, height, fps, bps, sample } = sampleHtmlVideoTech(video, techSampleRef.current, {
      downloadedBytes: downloadedBytesRef.current,
    })
    techSampleRef.current = sample
    const res = formatStreamResolution(width, height)
    if (res) setResolution(res)
    const br = formatStreamBitrate(bps > 0 ? bps : metaBitrateBpsRef.current)
    if (br) setBitrateLabel(br)
    const fpsText = formatStreamFps(fps)
    if (fpsText) setFpsLabel(fpsText)
    setBufferSec(Math.round(bufferedAhead(video) * 10) / 10)
  }, [])

  useEffect(() => {
    durationRef.current = duration
  }, [duration])

  useEffect(() => {
    if (durationSec && durationSec > 0) {
      setDuration(durationSec)
      durationRef.current = durationSec
    }
  }, [durationSec])

  // Fragmented remux pipes don't expose duration — probe the source MKV.
  useEffect(() => {
    if (!fileName) return
    let cancelled = false
    api
      .recordingInfo(fileName)
      .then((info) => {
        if (cancelled) return
        if (info.duration_ms && info.duration_ms > 0) {
          const sec = info.duration_ms / 1000
          setDuration(sec)
          durationRef.current = sec
        }
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [fileName])

  // VOD: duration + stored tech (bitrate/codec) from movie detail API.
  useEffect(() => {
    if (fileName) return
    const m = streamUrl.match(/\/api\/vod\/movies\/([^/?#]+)\/remux/)
    if (!m) return
    let cancelled = false
    api
      .vodMovie(m[1])
      .then((r) => {
        if (cancelled) return
        const fromApi = r.duration_sec && r.duration_sec > 0 ? r.duration_sec : 0
        const sec = fromApi > 0 ? fromApi : parseDurationSec(r.movie.duration)
        if (sec > 0 && !(durationSec && durationSec > 0)) {
          setDuration(sec)
          durationRef.current = sec
        }
        if (r.movie.video_codec) videoCodecHintRef.current = r.movie.video_codec
        const kbps = r.movie.bitrate_kbps || 0
        if (kbps > 0) {
          metaBitrateBpsRef.current = kbps * 1000
          setBitrateLabel(formatStreamBitrate(kbps * 1000))
        }
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [streamUrl, durationSec, fileName])

  useEffect(() => {
    const video = videoRef.current
    if (!video) return
    const url = `${streamUrl}${streamUrl.includes('?') ? '&' : '?'}start=${remuxStartRef.current.toFixed(3)}`
    setError('')
    setResolution('')
    setFpsLabel('')
    setBufferSec(0)
    downloadedBytesRef.current = 0
    techSampleRef.current = { t: 0, frames: 0, bytes: 0, rvfcFrames: 0, rvfcT: 0 }
    if (metaBitrateBpsRef.current > 0) {
      setBitrateLabel(formatStreamBitrate(metaBitrateBpsRef.current))
    } else {
      setBitrateLabel('')
    }

    const ac = new AbortController()
    let disposeMse: (() => void) | undefined

    const codecHint = videoCodecHintRef.current
    // empty_moov fMP4 remux pipes are not valid progressive media in Firefox
    // (NS_ERROR_DOM_MEDIA_RANGE_ERR). Always use MSE when available.
    if (remuxNeedsMediaSource() && canUseFmp4FetchPlayback(codecHint)) {
      disposeMse = attachFmp4FetchPlayback(video, url, {
        videoCodecHint: codecHint,
        signal: ac.signal,
        onBytes: (n) => {
          downloadedBytesRef.current = n
        },
        onError: (msg) => {
          setError(msg || 'Could not play this stream in the browser')
        },
      })
      void video.play().catch(() => undefined)
    } else {
      setError('This browser cannot play remuxed streams (MediaSource required)')
    }

    const onBufferUpdate = () => setBufferSec(Math.round(bufferedAhead(video) * 10) / 10)
    video.addEventListener('loadedmetadata', syncVideoStats)
    video.addEventListener('resize', syncVideoStats)
    video.addEventListener('progress', onBufferUpdate)
    video.addEventListener('timeupdate', onBufferUpdate)
    const statsPoll = window.setInterval(syncVideoStats, 1000)
    const stopFps = attachVideoFrameFps(video, (fps) => {
      const fpsText = formatStreamFps(fps)
      if (fpsText) setFpsLabel(fpsText)
    })
    return () => {
      ac.abort()
      disposeMse?.()
      stopFps()
      window.clearInterval(statsPoll)
      video.removeEventListener('loadedmetadata', syncVideoStats)
      video.removeEventListener('resize', syncVideoStats)
      video.removeEventListener('progress', onBufferUpdate)
      video.removeEventListener('timeupdate', onBufferUpdate)
    }
  }, [streamUrl, reloadToken, syncVideoStats])

  useEffect(() => () => stopVideoElement(videoRef.current), [])

  const toggleFullscreen = async () => {
    const el = rootRef.current
    if (!el) return
    try {
      if (document.fullscreenElement) await document.exitFullscreen()
      else {
        if (docked) onExpand()
        await el.requestFullscreen()
      }
    } catch {
      /* blocked */
    }
  }

  useEffect(() => {
    const syncFs = () => setFullscreen(document.fullscreenElement === rootRef.current)
    document.addEventListener('fullscreenchange', syncFs)
    return () => document.removeEventListener('fullscreenchange', syncFs)
  }, [])

  useEffect(() => {
    if (docked && document.fullscreenElement === rootRef.current) {
      void document.exitFullscreen().catch(() => undefined)
    }
  }, [docked])

  const togglePlay = useCallback(() => {
    const video = videoRef.current
    if (!video) return
    if (video.paused) void video.play()
    else video.pause()
  }, [])

  const commitSeek = useCallback((t: number) => {
    const max = durationRef.current
    const next = Math.max(0, max > 0 ? Math.min(t, Math.max(max - 0.25, 0)) : t)
    remuxStartRef.current = next
    setCurrent(next)
    setScrub(null)
    setReloadToken((n) => n + 1)
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !document.fullscreenElement) {
        if (!docked) onMinimize()
        return
      }
      if (e.key === ' ') {
        const tag = (e.target as HTMLElement | null)?.tagName
        if (tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement | null)?.isContentEditable) return
        e.preventDefault()
        togglePlay()
      }
      if ((e.key === 'f' || e.key === 'F') && !docked) {
        e.preventDefault()
        void toggleFullscreen()
      }
      if (e.key === 'ArrowRight') {
        e.preventDefault()
        commitSeek((scrub ?? current) + 10)
      }
      if (e.key === 'ArrowLeft') {
        e.preventDefault()
        commitSeek(Math.max(0, (scrub ?? current) - 10))
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [docked, onMinimize, togglePlay, commitSeek, scrub, current])

  const techBits = showTechInfo
    ? [
        resolution || null,
        fpsLabel || null,
        bitrateLabel || null,
        bufferSec > 0 ? `${bufferSec.toFixed(1)}s buffer` : null,
      ].filter(Boolean)
    : []

  const overlayClass = ['player-overlay', fullscreen ? 'is-fullscreen' : '', docked ? 'is-docked' : '']
    .filter(Boolean)
    .join(' ')
  const shellClass = ['player-shell', chromeOn ? 'chrome-on' : 'chrome-off'].join(' ')
  const label = title || fileName
  const displayTime = scrub ?? current
  const durationLabel = duration > 0 ? formatDuration(duration * 1000) : '—:—'

  return (
    <div
      className={overlayClass}
      ref={rootRef}
      role="dialog"
      aria-modal={!docked}
      aria-label="Recording player"
      onMouseMove={reveal}
      onPointerDown={reveal}
    >
      <div
        className={shellClass}
        onMouseLeave={() => {
          if (!chromePinned) scheduleHide()
        }}
      >
        <div
          className="player-stage"
          onDoubleClick={() => {
            if (docked) onExpand()
            else void toggleFullscreen()
          }}
          onClick={() => {
            if (docked) onExpand()
            else reveal()
          }}
        >
          {error && (
            <div className="player-status error">
              <p>{error}</p>
            </div>
          )}
          <video
            ref={videoRef}
            className="player-video"
            playsInline
            onPlay={() => setPlaying(true)}
            onPause={() => setPlaying(false)}
            onTimeUpdate={(e) => {
              if (scrub != null) return
              setCurrent(remuxStartRef.current + e.currentTarget.currentTime)
            }}
            onError={() => {
              // MSE blob teardown can fire a spurious error; only surface if still no source.
              const v = videoRef.current
              if (v && !v.currentSrc) setError('Could not play this stream in the browser')
            }}
            onVolumeChange={(e) => setMuted(e.currentTarget.muted || e.currentTarget.volume === 0)}
          />

          {!docked && (
            <div className="player-top player-chrome" onMouseEnter={hold} onFocusCapture={hold}>
              <div>
                <strong>{label}</strong>
                <span className="muted player-mode">{modeLabel}</span>
                {techBits.length > 0 && <span className="player-tech">{techBits.join(' · ')}</span>}
              </div>
              <div className="player-top-actions">
                <button type="button" className="player-icon-btn" onClick={onMinimize} title="Minimize to dock">
                  <Icon icon={icons.minimize} />
                </button>
                <button
                  type="button"
                  className="player-icon-btn"
                  onClick={() => {
                    stopVideoElement(videoRef.current)
                    onClose()
                  }}
                  title="Close"
                >
                  <Icon icon={icons.close} />
                </button>
              </div>
            </div>
          )}

          <div
            className="player-controls player-chrome"
            onMouseEnter={hold}
            onFocusCapture={hold}
            onMouseLeave={() => {
              if (!chromePinned) scheduleHide()
            }}
          >
            {docked && (
              <div className="player-dock-meta">
                <strong>{label}</strong>
                <span className="muted mono">
                  {formatDuration(displayTime * 1000)} / {durationLabel}
                  {showTechInfo && techBits.length > 0 ? ` · ${techBits.join(' · ')}` : ''}
                </span>
              </div>
            )}
            <input
              className="player-seek"
              type="range"
              min={0}
              max={Math.max(duration, 1)}
              step={0.1}
              disabled={duration <= 0}
              value={Math.min(displayTime, duration || 0)}
              onChange={(e) => setScrub(Number(e.target.value))}
              onPointerUp={() => {
                if (scrub != null) commitSeek(scrub)
              }}
              onKeyUp={(e) => {
                if (scrub == null) return
                if (e.key === 'ArrowLeft' || e.key === 'ArrowRight' || e.key === 'Home' || e.key === 'End') {
                  commitSeek(scrub)
                }
              }}
              aria-label="Seek"
            />
            <div className="player-bar">
              <button type="button" className="player-icon-btn" onClick={togglePlay} title={playing ? 'Pause' : 'Play'}>
                <Icon icon={playing ? icons.pause : icons.play} />
              </button>
              <button
                type="button"
                className="player-icon-btn"
                onClick={() => {
                  const video = videoRef.current
                  if (!video) return
                  video.muted = !video.muted
                  setMuted(video.muted)
                }}
                title={muted ? 'Unmute' : 'Mute'}
              >
                <Icon icon={muted ? icons.mute : icons.volume} />
              </button>
              {!docked && (
                <span className="player-time mono">
                  {formatDuration(displayTime * 1000)} / {durationLabel}
                </span>
              )}
              <div className="player-spacer" />
              {!docked && (
                <>
                  <button
                    type="button"
                    className="player-icon-btn"
                    title={fullscreen ? 'Exit fullscreen' : 'Fullscreen'}
                    onClick={() => void toggleFullscreen()}
                  >
                    <Icon icon={fullscreen ? icons.compress : icons.expand} />
                  </button>
                  <button type="button" className="player-icon-btn" onClick={onMinimize} title="Minimize to dock">
                    <Icon icon={icons.minimize} />
                  </button>
                </>
              )}
              {docked && (
                <button type="button" className="player-icon-btn" onClick={onExpand} title="Expand">
                  <Icon icon={icons.expandPlayer} />
                </button>
              )}
              <button
                type="button"
                className="player-icon-btn"
                onClick={() => {
                  stopVideoElement(videoRef.current)
                  onClose()
                }}
                title="Close"
              >
                <Icon icon={icons.close} />
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
