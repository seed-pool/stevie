import { useCallback, useEffect, useRef, useState } from 'react'
import Hls from 'hls.js'
import mpegts, { type Player as MpegtsPlayer } from 'mpegts.js'
import { api, notifyLiveRecordingChange } from './api'
import { Icon, icons } from './icons'
import {
  LIVE_MIN_BUFFER_SEC,
  LIVE_MPEGTS_CONFIG,
  LIVE_REBUFFER_SEC,
  bufferedAhead,
  preferHlsUrl,
  waitForBuffer,
} from './liveMpegts'
import {
  attachVideoFrameFps,
  formatStreamBitrate,
  formatStreamCodec,
  formatStreamFps,
  formatStreamResolution,
  getShowPlayerTechInfo,
  sampleHtmlVideoTech,
  type HtmlVideoTechSample,
} from './playerPrefs'
import { usePlayerChrome } from './usePlayerChrome'

type Props = {
  channelId: string
  /** Same-origin Stevie proxy URL (`/api/live/channels/.../stream`). */
  streamUrl: string
  title?: string
  channelName?: string
  mode: 'expanded' | 'docked'
  onClose: () => void
  onMinimize: () => void
  onExpand: () => void
}

function isHlsUrl(url: string) {
  // Same-origin live / streamed proxies serve rewritten HLS even without .m3u8 in the path.
  return (
    /\.m3u8(\?|$)/i.test(url) ||
    /\/api\/live\/channels\/[^/]+\/stream(?:\?|$)/i.test(url) ||
    /\/api\/sports\/streamed\/(playlist|hls)(?:\?|$)/i.test(url)
  )
}

export function LivePlayer({
  channelId,
  streamUrl,
  title,
  channelName,
  mode,
  onClose,
  onMinimize,
  onExpand,
}: Props) {
  const showChannel =
    !!channelName && !!title && channelName.trim().toLowerCase() !== title.trim().toLowerCase()
  const rootRef = useRef<HTMLDivElement>(null)
  const videoRef = useRef<HTMLVideoElement>(null)
  const mpegtsRef = useRef<MpegtsPlayer | null>(null)
  const hlsRef = useRef<Hls | null>(null)
  const [error, setError] = useState('')
  const [status, setStatus] = useState('Connecting…')
  const [playing, setPlaying] = useState(false)
  const [bufferSec, setBufferSec] = useState(0)
  const [resolution, setResolution] = useState('')
  const [bitrateLabel, setBitrateLabel] = useState('')
  const [fpsLabel, setFpsLabel] = useState('')
  const [codecLabel, setCodecLabel] = useState('')
  const [needsGesture, setNeedsGesture] = useState(false)
  const [fullscreen, setFullscreen] = useState(false)
  const [showTechInfo, setShowTechInfo] = useState(getShowPlayerTechInfo)
  const [recording, setRecording] = useState(false)
  const [recordBusy, setRecordBusy] = useState(false)
  const [recordHint, setRecordHint] = useState('')
  const [muted, setMuted] = useState(false)
  const techSampleRef = useRef<HtmlVideoTechSample>({ t: 0, frames: 0, bytes: 0, rvfcFrames: 0, rvfcT: 0 })
  const docked = mode === 'docked'
  const streamedFeed = parseStreamedChannelId(channelId)

  useEffect(() => {
    let cancelled = false
    setRecording(false)
    setRecordHint('')
    const poll = () =>
      streamedFeed
        ? api.streamedRecordingStatus(streamedFeed)
        : api.liveRecordingStatus(channelId)
    poll()
      .then((r) => {
        if (!cancelled) setRecording(!!r.recording)
      })
      .catch(() => {
        if (!cancelled) setRecording(false)
      })
    const t = window.setInterval(() => {
      poll()
        .then((r) => {
          if (!cancelled) setRecording(!!r.recording)
        })
        .catch(() => undefined)
    }, 4000)
    return () => {
      cancelled = true
      window.clearInterval(t)
    }
  }, [channelId, streamedFeed?.source, streamedFeed?.id, streamedFeed?.stream])

  const toggleRecord = async () => {
    if (recordBusy) return
    setRecordBusy(true)
    setRecordHint('')
    try {
      if (recording) {
        const r = streamedFeed
          ? await api.stopStreamedRecording(streamedFeed)
          : await api.stopLiveRecording(channelId)
        setRecording(false)
        setRecordHint(r.file_name ? `Saved ${r.file_name}` : 'Recording stopped')
      } else {
        const programTitle = title && title !== channelName ? title : undefined
        const r = streamedFeed
          ? await api.startStreamedRecording({
              ...streamedFeed,
              program_title: programTitle,
              channel_name: channelName || 'Streamed',
            })
          : await api.startLiveRecording(channelId, { program_title: programTitle })
        setRecording(true)
        setRecordHint(r.program_title ? `REC · ${r.program_title}` : 'Recording…')
      }
      notifyLiveRecordingChange()
    } catch (err) {
      setRecordHint(err instanceof Error ? err.message : 'Recording failed')
    } finally {
      setRecordBusy(false)
    }
  }

  const syncVideoStats = useCallback(() => {
    const video = videoRef.current
    if (!video) return

    let w = video.videoWidth
    let h = video.videoHeight
    let bps = 0
    let fps = 0
    let codec = ''

    const hls = hlsRef.current
    if (hls && hls.levels?.length) {
      const idx = hls.currentLevel >= 0 ? hls.currentLevel : hls.loadLevel
      const level = idx >= 0 ? hls.levels[idx] : undefined
      if (level) {
        if ((!w || !h) && level.width && level.height) {
          w = level.width
          h = level.height
        }
        bps = level.bitrate || 0
        const levelFps =
          typeof (level as { frameRate?: number }).frameRate === 'number'
            ? (level as { frameRate?: number }).frameRate!
            : Number(level.attrs?.['FRAME-RATE'] || 0)
        if (levelFps > 0) fps = levelFps
        codec =
          formatStreamCodec(level.videoCodec) ||
          formatStreamCodec((level as { codecSet?: string }).codecSet) ||
          ''
      }
      // Fallback: measured download bandwidth when playlist omits BANDWIDTH.
      if (!bps && typeof hls.bandwidthEstimate === 'number' && hls.bandwidthEstimate > 0) {
        bps = hls.bandwidthEstimate
      }
    }

    // Measured fps from decoded frames when the playlist omits FRAME-RATE (common for live).
    const measured = sampleHtmlVideoTech(video, techSampleRef.current)
    techSampleRef.current = measured.sample
    if (fps <= 0 && measured.fps > 0) fps = measured.fps
    if ((!w || !h) && measured.width && measured.height) {
      w = measured.width
      h = measured.height
    }

    const label = formatStreamResolution(w, h)
    if (label) setResolution(label)
    const br = formatStreamBitrate(bps)
    if (br) setBitrateLabel(br)
    const fpsText = formatStreamFps(fps)
    if (fpsText) setFpsLabel(fpsText)
    if (codec) setCodecLabel(codec)
  }, [])

  const tryPlay = async () => {
    const video = videoRef.current
    if (!video) return
    setNeedsGesture(false)
    try {
      video.muted = false
      setMuted(false)
      await video.play()
    } catch {
      // Autoplay with sound blocked after cross-origin navigation — start muted, then ask.
      try {
        video.muted = true
        setMuted(true)
        await video.play()
        setNeedsGesture(true)
      } catch {
        setNeedsGesture(true)
      }
    }
  }

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

  useEffect(() => {
    const video = videoRef.current
    if (!video) return

    let cancelled = false
    const ac = new AbortController()
    const playUrl = preferHlsUrl(streamUrl)
    setError('')
    setNeedsGesture(false)
    setStatus('Buffering stream…')
    setResolution('')
    setBitrateLabel('')
    setFpsLabel('')
    setCodecLabel('')
    setBufferSec(0)
    techSampleRef.current = { t: 0, frames: 0, bytes: 0, rvfcFrames: 0, rvfcT: 0 }

    const destroy = () => {
      if (mpegtsRef.current) {
        try {
          mpegtsRef.current.pause()
          mpegtsRef.current.unload()
          mpegtsRef.current.detachMediaElement()
          mpegtsRef.current.destroy()
        } catch {
          /* ignore */
        }
        mpegtsRef.current = null
      }
      if (hlsRef.current) {
        try {
          hlsRef.current.destroy()
        } catch {
          /* ignore */
        }
        hlsRef.current = null
      }
      video.removeAttribute('src')
      video.load()
    }

    ;(async () => {
      destroy()
      if (!playUrl) {
        setError('No stream URL')
        return
      }

      // Same-origin proxy: Stevie fetches IPTV server-side (works behind HTTPS).
      if (isHlsUrl(playUrl) && Hls.isSupported()) {
        const hls = new Hls({
          enableWorker: false,
          lowLatencyMode: false,
          backBufferLength: 30,
          maxBufferLength: 30,
          maxMaxBufferLength: 60,
          maxBufferSize: 60 * 1000 * 1000,
          maxBufferHole: 0.5,
          startLevel: -1,
          xhrSetup: (xhr) => {
            xhr.withCredentials = true
          },
        })
        hlsRef.current = hls
        hls.on(Hls.Events.ERROR, (_e, data) => {
          if (cancelled || !data.fatal) return
          // streamed.pk offline/expired tokens surface as manifestLoadError — not a concurrent-login issue.
          if (streamedFeed) {
            setError('Stream Offline')
          } else {
            setError(data.details || 'HLS playback error')
          }
          setStatus('')
        })
        hls.on(Hls.Events.LEVEL_SWITCHED, () => {
          if (!cancelled) syncVideoStats()
        })
        hls.on(Hls.Events.LEVEL_LOADED, () => {
          if (!cancelled) syncVideoStats()
        })
        hls.loadSource(playUrl)
        hls.attachMedia(video)
        hls.on(Hls.Events.MANIFEST_PARSED, () => {
          if (cancelled) return
          setStatus('')
          syncVideoStats()
          void tryPlay()
        })
        return
      }

      if (isHlsUrl(playUrl) && video.canPlayType('application/vnd.apple.mpegurl')) {
        video.src = playUrl
        video.addEventListener(
          'loadedmetadata',
          () => {
            if (cancelled) return
            setStatus('')
            syncVideoStats()
            void tryPlay()
          },
          { once: true },
        )
        return
      }

      if (!mpegts.isSupported()) {
        setError('This browser cannot play the live stream format')
        return
      }

      const player = mpegts.createPlayer(
        { type: 'mse', isLive: true, url: playUrl },
        { ...LIVE_MPEGTS_CONFIG },
      )
      mpegtsRef.current = player
      player.attachMediaElement(video)
      player.on(mpegts.Events.ERROR, (...args: unknown[]) => {
        if (cancelled) return
        const detail = args
          .map((a) => {
            if (a && typeof a === 'object' && 'msg' in a) return String((a as { msg?: string }).msg)
            return ''
          })
          .filter(Boolean)
          .join(' ')
        setError(detail || 'Live stream error')
        setStatus('')
      })
      const statsEvent = (mpegts.Events as { STATISTICS_INFO?: string }).STATISTICS_INFO
      if (statsEvent) {
        player.on(statsEvent, (info: unknown) => {
          if (cancelled) return
          const stats = info as { speed?: number }
          // mpegts.js reports download speed in KB/s; approximate stream bitrate.
          if (typeof stats?.speed === 'number' && stats.speed > 0) {
            const br = formatStreamBitrate(stats.speed * 1000 * 8)
            if (br) setBitrateLabel(br)
          }
          syncVideoStats()
        })
      }
      player.load()

      const ready = await waitForBuffer(video, LIVE_MIN_BUFFER_SEC, 30_000, ac.signal)
      if (cancelled) return
      if (!ready && bufferedAhead(video) < 0.4) {
        setError('Timed out waiting for stream buffer')
        setStatus('')
        return
      }
      setStatus('')
      syncVideoStats()
      void tryPlay()
    })()

    const onStall = () => {
      if (cancelled) return
      if (bufferedAhead(video) >= LIVE_REBUFFER_SEC) return
      setStatus('Rebuffering…')
      video.pause()
      void waitForBuffer(video, LIVE_MIN_BUFFER_SEC, 20_000, ac.signal).then((ok) => {
        if (cancelled) return
        setStatus('')
        if (ok) void tryPlay()
      })
    }
    // Buffer readout is cheap; resolution/bitrate sync only on geometry / level events (+ light poll).
    const onBufferUpdate = () => setBufferSec(Math.round(bufferedAhead(video) * 10) / 10)
    video.addEventListener('waiting', onStall)
    video.addEventListener('stalled', onStall)
    video.addEventListener('progress', onBufferUpdate)
    video.addEventListener('timeupdate', onBufferUpdate)
    video.addEventListener('loadedmetadata', syncVideoStats)
    video.addEventListener('resize', syncVideoStats)
    const statsPoll = window.setInterval(() => {
      if (!cancelled) syncVideoStats()
    }, 2000)
    const stopFps = attachVideoFrameFps(video, (fps) => {
      if (cancelled) return
      const fpsText = formatStreamFps(fps)
      if (fpsText) setFpsLabel(fpsText)
    })

    return () => {
      cancelled = true
      ac.abort()
      stopFps()
      window.clearInterval(statsPoll)
      video.removeEventListener('waiting', onStall)
      video.removeEventListener('stalled', onStall)
      video.removeEventListener('progress', onBufferUpdate)
      video.removeEventListener('timeupdate', onBufferUpdate)
      video.removeEventListener('loadedmetadata', syncVideoStats)
      video.removeEventListener('resize', syncVideoStats)
      destroy()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [channelId, streamUrl])

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
      /* browser blocked fullscreen */
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
      if (e.key === 'm' || e.key === 'M') {
        const tag = (e.target as HTMLElement | null)?.tagName
        if (tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement | null)?.isContentEditable) return
        e.preventDefault()
        toggleMute()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [docked, onMinimize, muted])

  const togglePlay = () => {
    const video = videoRef.current
    if (!video) return
    if (video.paused || needsGesture) void tryPlay()
    else video.pause()
  }

  const toggleMute = () => {
    const video = videoRef.current
    if (!video) return
    video.muted = !video.muted
    setMuted(video.muted)
  }

  const unmuteAndPlay = () => {
    const video = videoRef.current
    if (video) {
      video.muted = false
      setMuted(false)
    }
    void tryPlay()
  }

  const chromePinned = !!(error || needsGesture || status)
  const { chromeOn, reveal, hold, scheduleHide } = usePlayerChrome({
    pinned: chromePinned,
    docked,
  })

  const techBits = showTechInfo
    ? [
        recording ? 'REC' : null,
        resolution || null,
        fpsLabel || null,
        bitrateLabel || null,
        codecLabel || null,
        bufferSec > 0 ? `${bufferSec.toFixed(1)}s buffer` : null,
      ].filter(Boolean)
    : recording
      ? ['REC']
      : []

  const overlayClass = ['player-overlay', fullscreen ? 'is-fullscreen' : '', docked ? 'is-docked' : '']
    .filter(Boolean)
    .join(' ')

  const shellClass = ['player-shell', chromeOn ? 'chrome-on' : 'chrome-off'].join(' ')

  return (
    <div
      className={overlayClass}
      ref={rootRef}
      role="dialog"
      aria-modal={!docked}
      aria-label="Live player"
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
          {status && !error && !needsGesture && <div className="player-status">{status}</div>}
          {needsGesture && !error && (
            <div className="player-status">
              <button type="button" className="ghost" onClick={unmuteAndPlay}>
                <Icon icon={icons.play} /> Play
              </button>
            </div>
          )}
          {error && (
            <div className="player-status error">
              <p>{error}</p>
              {!streamedFeed && (
                <>
                  <p className="muted" style={{ marginTop: '0.5rem' }}>
                    Close other IPTV apps first (this account allows 1 connection), then retry.
                  </p>
                  <button
                    type="button"
                    className="ghost"
                    style={{ marginTop: '0.75rem' }}
                    onClick={() => {
                      window.open(
                        `/api/live/channels/${channelId}/external.m3u`,
                        '_blank',
                        'noopener,noreferrer',
                      )
                    }}
                  >
                    <Icon icon={icons.external} /> Open in VLC
                  </button>
                </>
              )}
            </div>
          )}
          <video
            ref={videoRef}
            className="player-video"
            playsInline
            preload="auto"
            onPlay={() => setPlaying(true)}
            onPause={() => setPlaying(false)}
          />

          {!docked && (
            <div className="player-top player-chrome" onMouseEnter={hold} onFocusCapture={hold}>
              <div>
                <strong>{title || channelName || 'Live TV'}</strong>
                <span className="muted"> · Live</span>
                {showChannel && <span className="muted"> · {channelName}</span>}
                {techBits.length > 0 && <span className="player-tech">{techBits.join(' · ')}</span>}
              </div>
              <div className="player-top-actions">
                <button
                  type="button"
                  className="player-icon-btn"
                  title="Open in VLC"
                  onClick={() => {
                    window.open(`/api/live/channels/${channelId}/external.m3u`, '_blank', 'noopener,noreferrer')
                  }}
                >
                  <Icon icon={icons.external} />
                </button>
                <button type="button" className="player-icon-btn" onClick={onMinimize} title="Minimize to dock">
                  <Icon icon={icons.minimize} />
                </button>
                <button type="button" className="player-icon-btn" onClick={onClose} title="Close">
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
                <strong>{title || channelName || 'Live TV'}</strong>
                <span className="muted">
                  {needsGesture
                    ? showChannel
                      ? `${channelName} · Tap play`
                      : 'Tap play'
                    : showChannel
                      ? channelName
                      : status || (showTechInfo && techBits.length ? techBits.join(' · ') : 'Live')}
                </span>
              </div>
            )}
            <div className="player-bar">
              <button
                type="button"
                className="player-icon-btn"
                onClick={togglePlay}
                title={playing && !needsGesture ? 'Pause' : 'Play'}
              >
                <Icon icon={playing && !needsGesture ? icons.pause : icons.play} />
              </button>
              <button
                type="button"
                className={['player-icon-btn', 'player-record-btn', recording ? 'is-recording' : '']
                  .filter(Boolean)
                  .join(' ')}
                onClick={() => void toggleRecord()}
                disabled={recordBusy}
                title={recording ? 'Stop recording' : 'Record (remux to MKV)'}
              >
                <Icon icon={icons.record} />
              </button>
              <button
                type="button"
                className={['player-icon-btn', muted ? 'is-muted' : ''].filter(Boolean).join(' ')}
                onClick={toggleMute}
                title={muted ? 'Unmute (M)' : 'Mute (M)'}
                aria-pressed={muted}
              >
                <Icon icon={muted ? icons.mute : icons.volume} />
              </button>
              {recordHint && !docked && <span className="player-record-hint muted">{recordHint}</span>}
              <div className="player-spacer" />
              {!docked && (
                <button
                  type="button"
                  className="player-icon-btn"
                  title={fullscreen ? 'Exit fullscreen' : 'Fullscreen'}
                  onClick={() => void toggleFullscreen()}
                >
                  <Icon icon={fullscreen ? icons.compress : icons.expand} />
                </button>
              )}
              {docked ? (
                <button type="button" className="player-icon-btn" onClick={onExpand} title="Expand">
                  <Icon icon={icons.expandPlayer} />
                </button>
              ) : (
                <button type="button" className="player-icon-btn" onClick={onMinimize} title="Minimize to dock">
                  <Icon icon={icons.minimize} />
                </button>
              )}
              <button type="button" className="player-icon-btn" onClick={onClose} title="Close">
                <Icon icon={icons.close} />
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

/** `streamed:{source}:{id}:{streamNo}` from PlayerContext.playStreamed */
function parseStreamedChannelId(
  channelId: string,
): { source: string; id: string; stream: number } | null {
  if (!channelId.startsWith('streamed:')) return null
  const parts = channelId.split(':')
  // streamed : source : id : stream  (id may contain colons in theory — keep last as stream)
  if (parts.length < 3) return null
  const source = parts[1]
  let stream = 1
  let idParts = parts.slice(2)
  if (idParts.length >= 2 && /^\d+$/.test(idParts[idParts.length - 1] || '')) {
    stream = Number(idParts[idParts.length - 1])
    idParts = idParts.slice(0, -1)
  }
  const id = idParts.join(':')
  if (!source || !id) return null
  return { source, id, stream }
}
