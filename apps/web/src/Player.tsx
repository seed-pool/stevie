import { useEffect, useMemo, useRef, useState } from 'react'
import {
  api,
  formatDuration,
  type MediaFile,
  type PlaybackDecision,
  type PlaybackInfo,
} from './api'
import { capsQuery, detectClientCaps } from './capabilities'
import { attachFmp4FetchPlayback, canUseFmp4FetchPlayback, remuxNeedsMediaSource } from './fmp4FetchPlayback'
import { Icon, icons } from './icons'
import { usePlayerChrome } from './usePlayerChrome'

type Props = {
  media: MediaFile
  title?: string
  mode: 'expanded' | 'docked'
  onClose: () => void
  onMinimize: () => void
  onExpand: () => void
}

const LANG_NAMES: Record<string, string> = {
  eng: 'English',
  en: 'English',
  spa: 'Spanish',
  es: 'Spanish',
  fre: 'French',
  fra: 'French',
  fr: 'French',
  ger: 'German',
  deu: 'German',
  de: 'German',
  ita: 'Italian',
  it: 'Italian',
  por: 'Portuguese',
  pt: 'Portuguese',
  pob: 'Portuguese (BR)',
  jpn: 'Japanese',
  ja: 'Japanese',
  kor: 'Korean',
  ko: 'Korean',
  chi: 'Chinese',
  zho: 'Chinese',
  zh: 'Chinese',
  rus: 'Russian',
  ru: 'Russian',
  ara: 'Arabic',
  ar: 'Arabic',
  hin: 'Hindi',
  hi: 'Hindi',
  cze: 'Czech',
  ces: 'Czech',
  cs: 'Czech',
  dan: 'Danish',
  da: 'Danish',
  gre: 'Greek',
  ell: 'Greek',
  el: 'Greek',
  fin: 'Finnish',
  fi: 'Finnish',
  fil: 'Filipino',
  tgl: 'Filipino',
  hrv: 'Croatian',
  hr: 'Croatian',
  hun: 'Hungarian',
  hu: 'Hungarian',
  ind: 'Indonesian',
  id: 'Indonesian',
  may: 'Malay',
  msa: 'Malay',
  ms: 'Malay',
  nob: 'Norwegian',
  nor: 'Norwegian',
  no: 'Norwegian',
  nno: 'Norwegian',
  dut: 'Dutch',
  nld: 'Dutch',
  nl: 'Dutch',
  pol: 'Polish',
  pl: 'Polish',
  rum: 'Romanian',
  ron: 'Romanian',
  ro: 'Romanian',
  swe: 'Swedish',
  sv: 'Swedish',
  tha: 'Thai',
  th: 'Thai',
  tur: 'Turkish',
  tr: 'Turkish',
  ukr: 'Ukrainian',
  uk: 'Ukrainian',
  vie: 'Vietnamese',
  vi: 'Vietnamese',
  und: 'Unknown',
}

function languageName(code?: string) {
  if (!code) return 'Unknown'
  const key = code.trim().toLowerCase()
  return LANG_NAMES[key] || code.toUpperCase()
}

function trackLabel(t: {
  language?: string
  title?: string
  codec?: string
  channels?: number
  index: number
  forced?: boolean
  commentary?: boolean
  text_based?: boolean
  default?: boolean
}) {
  const bits = [
    languageName(t.language),
    t.title && t.title.toLowerCase() !== (t.language || '').toLowerCase() ? t.title : undefined,
    t.commentary ? 'Commentary' : undefined,
    t.forced ? 'Forced' : undefined,
    t.default ? 'Default' : undefined,
    t.codec?.toUpperCase(),
    t.channels ? `${t.channels}ch` : undefined,
    t.text_based === false ? 'image' : undefined,
  ].filter(Boolean)
  return bits.join(' · ')
}

function shortTrackLabel(t: { language?: string; title?: string; commentary?: boolean; codec?: string }) {
  if (t.commentary) return 'Commentary'
  if (t.title) return t.title.length > 28 ? `${t.title.slice(0, 26)}…` : t.title
  return languageName(t.language)
}

function stopVideoElement(video: HTMLVideoElement | null) {
  if (!video) return
  try {
    video.pause()
    video.removeAttribute('src')
    video.load() // aborts in-flight media/remux requests
  } catch {
    /* ignore */
  }
}

export function Player({ media, title, mode, onClose, onMinimize, onExpand }: Props) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const rootRef = useRef<HTMLDivElement>(null)
  const remuxStartRef = useRef(0)
  const caps = useMemo(() => detectClientCaps(), [])
  const docked = mode === 'docked'

  const handleClose = () => {
    stopVideoElement(videoRef.current)
    onClose()
  }

  useEffect(() => {
    return () => stopVideoElement(videoRef.current)
  }, [])

  const [info, setInfo] = useState<PlaybackInfo | null>(null)
  const [decision, setDecision] = useState<PlaybackDecision | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [playing, setPlaying] = useState(false)
  const [muted, setMuted] = useState(false)
  const [current, setCurrent] = useState(0)
  const [duration, setDuration] = useState((media.duration_ms ?? 0) / 1000)
  const [audioIndex, setAudioIndex] = useState(-1)
  const [subtitleIndex, setSubtitleIndex] = useState<number | 'off'>('off')
  const [menu, setMenu] = useState<'audio' | 'subs' | null>(null)
  const [fullscreen, setFullscreen] = useState(false)
  const [reloadToken, setReloadToken] = useState(0)
  const [timelineOrigin, setTimelineOrigin] = useState(0)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    const q = `${capsQuery(caps)}${audioIndex >= 0 ? `&audio=${audioIndex}` : ''}`
    api
      .playback(media.id, q)
      .then((r) => {
        if (cancelled) return
        setInfo(r)
        setDecision(r.decision)
        if (audioIndex < 0) setAudioIndex(r.decision.selected_audio_index)
        if (r.decision.mode === 'unsupported') {
          setError(r.decision.reason || 'Playback not supported in this browser')
        }
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Playback failed')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [media.id, caps, audioIndex])

  const usesRemux = (d: PlaybackDecision | null, audio: number) => {
    if (!d || d.mode === 'unsupported') return false
    if (d.mode === 'remux') return true
    return audio >= 0 && audio !== d.default_audio_index
  }

  useEffect(() => {
    const video = videoRef.current
    if (!video || !info || !decision || decision.mode === 'unsupported') return
    const audio = audioIndex >= 0 ? audioIndex : decision.selected_audio_index
    let url = info.urls.stream
    const remux = usesRemux(decision, audio)
    if (remux) {
      url = `${info.urls.remux}?${capsQuery(caps)}&audio=${audio}&start=${remuxStartRef.current.toFixed(3)}`
    }

    const ac = new AbortController()
    let disposeMse: (() => void) | undefined

    if (remux && remuxNeedsMediaSource() && canUseFmp4FetchPlayback()) {
      disposeMse = attachFmp4FetchPlayback(video, url, {
        signal: ac.signal,
        onError: (msg) => setError(msg || 'Could not play remux stream'),
      })
      void video.play().catch(() => undefined)
    } else if (remux && remuxNeedsMediaSource()) {
      setError('This browser cannot play remuxed streams (MediaSource required)')
    } else {
      video.src = url
      video.load()
      void video.play().catch(() => undefined)
    }

    return () => {
      ac.abort()
      disposeMse?.()
    }
  }, [info, decision, audioIndex, caps, reloadToken])

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
      // keep close via handleClose only from buttons
      if (e.key === ' ') {
        const tag = (e.target as HTMLElement | null)?.tagName
        if (tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement | null)?.isContentEditable) return
        e.preventDefault()
        const video = videoRef.current
        if (!video) return
        if (video.paused) void video.play()
        else video.pause()
      }
      if ((e.key === 'f' || e.key === 'F') && !docked) {
        e.preventDefault()
        void toggleFullscreen()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [docked, onMinimize])

  useEffect(() => {
    const video = videoRef.current
    if (!video) return
    const apply = () => {
      const tracks = video.textTracks
      for (let i = 0; i < tracks.length; i++) tracks[i].mode = 'disabled'
      if (subtitleIndex === 'off') return
      const els = video.querySelectorAll('track')
      for (let i = 0; i < els.length; i++) {
        const idx = Number((els[i] as HTMLTrackElement).dataset.index)
        if (idx === subtitleIndex && tracks[i]) tracks[i].mode = 'showing'
      }
    }
    apply()
    // Tracks load async; re-apply when cues become available.
    const onLoad = () => apply()
    video.addEventListener('loadedmetadata', onLoad)
    const tracks = video.textTracks
    for (let i = 0; i < tracks.length; i++) {
      tracks[i].addEventListener('load', onLoad)
    }
    return () => {
      video.removeEventListener('loadedmetadata', onLoad)
      for (let i = 0; i < tracks.length; i++) {
        tracks[i].removeEventListener('load', onLoad)
      }
    }
  }, [subtitleIndex, reloadToken, info, timelineOrigin])

  const togglePlay = () => {
    const video = videoRef.current
    if (!video) return
    if (video.paused) void video.play()
    else video.pause()
  }

  const onSeek = (value: number) => {
    const video = videoRef.current
    if (!video || !decision) return
    const audio = audioIndex >= 0 ? audioIndex : decision.selected_audio_index
    if (!usesRemux(decision, audio)) {
      video.currentTime = value
      setCurrent(value)
      return
    }
    remuxStartRef.current = value
    setTimelineOrigin(value)
    setCurrent(value)
    setReloadToken((n) => n + 1)
  }

  const openExternal = async () => {
    try {
      const tok = await api.playbackToken(media.id)
      window.open(tok.external_playlist_url, '_blank', 'noopener,noreferrer')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not create external playlist')
    }
  }

  const audioTracks = decision?.audio_tracks ?? []
  const allSubs = decision?.subtitle_tracks ?? []
  const selectedSub = allSubs.find((t) => t.index === subtitleIndex && t.text_based)
  const audio = audioIndex >= 0 ? audioIndex : decision?.selected_audio_index ?? -1
  const currentAudio = audioTracks.find((t) => t.index === audio)
  const remuxing = usesRemux(decision, audio)
  const vttStart = remuxing ? timelineOrigin : 0

  const audioMenu = audioTracks.length > 0 && (
    <div className="player-menu-wrap">
      <button
        type="button"
        className={menu === 'audio' ? 'player-icon-btn active player-track-btn' : 'player-icon-btn player-track-btn'}
        onClick={() => setMenu((m) => (m === 'audio' ? null : 'audio'))}
        title="Audio track"
      >
        <Icon icon={icons.audio} />
        {currentAudio && <span className="player-track-label">{shortTrackLabel(currentAudio)}</span>}
      </button>
      {menu === 'audio' && (
        <div className="player-menu">
          {audioTracks.map((t) => (
            <button
              key={t.index}
              type="button"
              className={t.index === audioIndex ? 'active' : ''}
              onClick={() => {
                remuxStartRef.current = current
                setTimelineOrigin(current)
                setAudioIndex(t.index)
                setMenu(null)
              }}
            >
              {trackLabel(t)}
              {!t.browser_safe || (t.codec && !['aac', 'mp3', 'ac3', 'eac3', 'opus', 'alac'].includes(t.codec))
                ? ' · convert'
                : ''}
            </button>
          ))}
        </div>
      )}
    </div>
  )

  const currentSubLabel =
    subtitleIndex === 'off' ? 'Off' : selectedSub ? shortTrackLabel(selectedSub) : 'Subs'

  const subsMenu = (
    <div className="player-menu-wrap">
      <button
        type="button"
        className={menu === 'subs' ? 'player-icon-btn active player-track-btn' : 'player-icon-btn player-track-btn'}
        onClick={() => setMenu((m) => (m === 'subs' ? null : 'subs'))}
        title="Subtitles"
      >
        <Icon icon={icons.captions} />
        <span className="player-track-label">
          {allSubs.length ? `${currentSubLabel} (${allSubs.length})` : 'Subs'}
        </span>
      </button>
      {menu === 'subs' && (
        <div className="player-menu player-menu-tall">
          <button
            type="button"
            className={subtitleIndex === 'off' ? 'active' : ''}
            onClick={() => {
              setSubtitleIndex('off')
              setMenu(null)
            }}
          >
            Off
          </button>
          {allSubs.map((t) => (
            <button
              key={t.index}
              type="button"
              className={subtitleIndex === t.index ? 'active' : ''}
              disabled={!t.text_based}
              title={t.text_based ? trackLabel(t) : 'Image-based subtitles need an external player'}
              onClick={() => {
                if (!t.text_based) return
                setSubtitleIndex(t.index)
                setMenu(null)
              }}
            >
              {trackLabel(t)}
              {!t.text_based ? ' · external only' : ''}
            </button>
          ))}
          {!allSubs.length && <div className="muted">No subtitle tracks</div>}
        </div>
      )}
    </div>
  )

  const chromePinned = !!(menu || error || (loading && !docked) || !playing)
  const { chromeOn, reveal, hold, scheduleHide } = usePlayerChrome({
    pinned: chromePinned,
    docked,
  })

  const overlayClass = [
    'player-overlay',
    fullscreen ? 'is-fullscreen' : '',
    docked ? 'is-docked' : '',
  ]
    .filter(Boolean)
    .join(' ')

  const shellClass = ['player-shell', chromeOn ? 'chrome-on' : 'chrome-off'].join(' ')

  return (
    <div
      className={overlayClass}
      ref={rootRef}
      role="dialog"
      aria-modal={!docked}
      aria-label="Player"
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
          {loading && !docked && <div className="player-status">Loading…</div>}
          {error && (
            <div className="player-status error">
              <p>{error}</p>
              <button type="button" onClick={openExternal}>
                <Icon icon={icons.external} /> Open in external player
              </button>
            </div>
          )}
          <video
            ref={videoRef}
            className="player-video"
            playsInline
            onPlay={() => setPlaying(true)}
            onPause={() => setPlaying(false)}
            onTimeUpdate={(e) => {
              const start = remuxStartRef.current
              const audio = audioIndex >= 0 ? audioIndex : decision?.selected_audio_index ?? -1
              if (usesRemux(decision, audio)) setCurrent(start + e.currentTarget.currentTime)
              else setCurrent(e.currentTarget.currentTime)
            }}
            onLoadedMetadata={(e) => {
              if (media.duration_ms) setDuration(media.duration_ms / 1000)
              else if (e.currentTarget.duration && Number.isFinite(e.currentTarget.duration)) {
                setDuration(e.currentTarget.duration + remuxStartRef.current)
              }
            }}
            onVolumeChange={(e) => setMuted(e.currentTarget.muted || e.currentTarget.volume === 0)}
          >
            {/* Only load the active track — attaching every language fires N ffmpeg jobs. */}
            {selectedSub && (
              <track
                key={`${selectedSub.index}-${vttStart.toFixed(3)}`}
                kind="subtitles"
                src={`/api/media/${media.id}/subtitles/${selectedSub.index}.vtt?start=${vttStart.toFixed(3)}`}
                srcLang={selectedSub.language || 'und'}
                label={trackLabel(selectedSub)}
                data-index={selectedSub.index}
                default
              />
            )}
          </video>

          {!docked && (
            <div className="player-top player-chrome" onMouseEnter={hold} onFocusCapture={hold}>
              <div>
                <strong>{title || 'Playback'}</strong>
                {decision && (
                  <span className="muted player-mode">
                    {decision.mode === 'direct' ? 'Direct play' : decision.mode === 'remux' ? 'Remux' : 'Unavailable'}
                    {decision.transcode_audio ? ' · audio converted' : ''}
                  </span>
                )}
              </div>
              <div className="player-top-actions">
                <button type="button" className="player-icon-btn" onClick={openExternal} title="External player">
                  <Icon icon={icons.external} />
                </button>
                <button type="button" className="player-icon-btn" onClick={onMinimize} title="Minimize to dock">
                  <Icon icon={icons.minimize} />
                </button>
                <button type="button" className="player-icon-btn" onClick={handleClose} title="Close">
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
                <strong>{title || 'Now playing'}</strong>
                <span className="muted mono">
                  {formatDuration(current * 1000)} / {formatDuration(duration * 1000)}
                </span>
              </div>
            )}
            <input
              className="player-seek"
              type="range"
              min={0}
              max={Math.max(duration, 1)}
              step={0.1}
              value={Math.min(current, duration || 0)}
              onChange={(e) => onSeek(Number(e.target.value))}
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
                  {formatDuration(current * 1000)} / {formatDuration(duration * 1000)}
                </span>
              )}
              <div className="player-spacer" />

              {audioMenu}
              {subsMenu}

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
                <button type="button" className="player-icon-btn" onClick={onExpand} title="Expand player">
                  <Icon icon={icons.expandPlayer} />
                </button>
              )}

              <button type="button" className="player-icon-btn" onClick={handleClose} title="Close">
                <Icon icon={icons.close} />
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
