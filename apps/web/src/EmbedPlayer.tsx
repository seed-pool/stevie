import { useEffect, useRef, useState } from 'react'
import { Icon, icons } from './icons'
import { usePlayerChrome } from './usePlayerChrome'

type Props = {
  embedUrl: string
  title?: string
  channelName?: string
  mode: 'expanded' | 'docked'
  onClose: () => void
  onMinimize: () => void
  onExpand: () => void
}

export function EmbedPlayer({
  embedUrl,
  title,
  channelName,
  mode,
  onClose,
  onMinimize,
  onExpand,
}: Props) {
  const rootRef = useRef<HTMLDivElement>(null)
  const [fullscreen, setFullscreen] = useState(false)
  const docked = mode === 'docked'
  const showChannel =
    !!channelName && !!title && channelName.trim().toLowerCase() !== title.trim().toLowerCase()

  const { chromeOn, reveal, hold, scheduleHide } = usePlayerChrome({
    pinned: false,
    docked,
  })

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

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !document.fullscreenElement) {
        if (!docked) onMinimize()
        return
      }
      if ((e.key === 'f' || e.key === 'F') && !docked) {
        e.preventDefault()
        void toggleFullscreen()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [docked, onMinimize])

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
      aria-label="Streamed player"
      onMouseMove={reveal}
      onPointerDown={reveal}
    >
      <div
        className={shellClass}
        onMouseLeave={() => {
          scheduleHide()
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
          <iframe
            className="player-video player-embed-frame"
            src={embedUrl}
            title={title || channelName || 'Streamed'}
            allow="autoplay; fullscreen; encrypted-media; picture-in-picture"
            allowFullScreen
            referrerPolicy="no-referrer-when-downgrade"
          />

          {!docked && (
            <div className="player-top player-chrome" onMouseEnter={hold} onFocusCapture={hold}>
              <div>
                <strong>{title || channelName || 'Streamed'}</strong>
                <span className="muted"> · Streamed</span>
                {showChannel && <span className="muted"> · {channelName}</span>}
              </div>
              <div className="player-top-actions">
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
            onMouseLeave={() => scheduleHide()}
          >
            {docked && (
              <div className="player-dock-meta">
                <strong>{title || channelName || 'Streamed'}</strong>
                <span className="muted">{showChannel ? channelName : 'Streamed'}</span>
              </div>
            )}
            <div className="player-bar">
              <span className="player-record-hint muted">External stream · recording unavailable</span>
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
