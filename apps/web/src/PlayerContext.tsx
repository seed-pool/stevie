import { createContext, useContext, useMemo, useState, type ReactNode } from 'react'
import { api, type MediaFile } from './api'
import { EmbedPlayer } from './EmbedPlayer'
import { LivePlayer } from './LivePlayer'
import { Player } from './Player'
import { RecordingPlayer } from './RecordingPlayer'

export type MediaPlayerSession = {
  kind: 'media'
  media: MediaFile
  title?: string
}

export type LivePlayerSession = {
  kind: 'live'
  channelId: string
  title?: string
  /** Library channel name — shown under the title when watching a named event (e.g. sports). */
  channelName?: string
  streamUrl: string
}

export type RecordingPlayerSession = {
  kind: 'recording'
  fileName: string
  title?: string
  streamUrl: string
}

export type VodPlayerSession = {
  kind: 'vod'
  streamUrl: string
  title?: string
  durationSec?: number
}

export type EmbedPlayerSession = {
  kind: 'embed'
  embedUrl: string
  title?: string
  channelName?: string
}

export type PlayerSession =
  | MediaPlayerSession
  | LivePlayerSession
  | RecordingPlayerSession
  | VodPlayerSession
  | EmbedPlayerSession

type PlayerMode = 'expanded' | 'docked'

type PlayerContextValue = {
  session: PlayerSession | null
  mode: PlayerMode
  play: (session: PlayerSession | { media: MediaFile; title?: string }) => void
  playLive: (channelId: string, title?: string) => void
  playEmbed: (embedUrl: string, title?: string, channelName?: string) => void
  /** Ad-free streamed.pk HLS via Stevie relay (source/id/streamNo). */
  playStreamed: (
    opts: { source: string; id: string; stream?: number },
    title?: string,
    channelName?: string,
  ) => void
  playRecording: (fileName: string, title?: string) => void
  playVod: (streamUrl: string, title?: string, durationSec?: number) => void
  close: () => void
  minimize: () => void
  expand: () => void
}

const PlayerContext = createContext<PlayerContextValue | null>(null)

function normalizeSession(session: PlayerSession | { media: MediaFile; title?: string }): PlayerSession {
  if ('kind' in session) return session
  return { kind: 'media', media: session.media, title: session.title }
}

export function PlayerProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<PlayerSession | null>(null)
  const [mode, setMode] = useState<PlayerMode>('expanded')

  const value = useMemo<PlayerContextValue>(
    () => ({
      session,
      mode,
      play: (next) => {
        setSession(normalizeSession(next))
        setMode('expanded')
      },
      playLive: (channelId, title) => {
        void (async () => {
          try {
            const { channel } = await api.liveChannel(channelId)
            setSession({
              kind: 'live',
              channelId,
              title: title || channel.name,
              channelName: channel.name,
              streamUrl: api.liveStreamUrl(channelId),
            })
            setMode('expanded')
          } catch (err) {
            console.error(err)
            alert(err instanceof Error ? err.message : 'Could not start live channel')
          }
        })()
      },
      playEmbed: (embedUrl, title, channelName) => {
        const url = embedUrl.trim()
        if (!url) return
        setSession({
          kind: 'embed',
          embedUrl: url,
          title,
          channelName: channelName || 'Streamed',
        })
        setMode('expanded')
      },
      playStreamed: (opts, title, channelName) => {
        void (async () => {
          try {
            const q = new URLSearchParams({
              source: opts.source,
              id: opts.id,
              stream: String(opts.stream && opts.stream > 0 ? opts.stream : 1),
            })
            const streamNo = opts.stream && opts.stream > 0 ? opts.stream : 1
            const r = await api.streamedPlay(q)
            setSession({
              kind: 'live',
              channelId: `streamed:${opts.source}:${opts.id}:${streamNo}`,
              title: title || channelName || 'Streamed',
              channelName: channelName || 'Streamed',
              streamUrl: r.stream_url,
            })
            setMode('expanded')
          } catch (err) {
            console.error(err)
            alert(err instanceof Error ? err.message : 'Could not start Streamed feed')
          }
        })()
      },
      playRecording: (fileName, title) => {
        setSession({
          kind: 'recording',
          fileName,
          title: title || fileName,
          streamUrl: api.recordingRemuxUrl(fileName),
        })
        setMode('expanded')
      },
      playVod: (streamUrl, title, durationSec) => {
        setSession({
          kind: 'vod',
          streamUrl,
          title: title || 'Playback',
          durationSec: durationSec && durationSec > 0 ? durationSec : undefined,
        })
        setMode('expanded')
      },
      close: () => {
        setSession(null)
        setMode('expanded')
      },
      minimize: () => setMode('docked'),
      expand: () => setMode('expanded'),
    }),
    [session, mode],
  )

  return (
    <PlayerContext.Provider value={value}>
      {children}
      {session?.kind === 'media' && (
        <Player
          key={session.media.id}
          media={session.media}
          title={session.title}
          mode={mode}
          onClose={value.close}
          onMinimize={value.minimize}
          onExpand={value.expand}
        />
      )}
      {session?.kind === 'live' && (
        <LivePlayer
          key={session.channelId}
          channelId={session.channelId}
          streamUrl={session.streamUrl}
          title={session.title}
          channelName={session.channelName}
          mode={mode}
          onClose={value.close}
          onMinimize={value.minimize}
          onExpand={value.expand}
        />
      )}
      {session?.kind === 'recording' && (
        <RecordingPlayer
          key={session.fileName}
          fileName={session.fileName}
          streamUrl={session.streamUrl}
          title={session.title}
          mode={mode}
          onClose={value.close}
          onMinimize={value.minimize}
          onExpand={value.expand}
        />
      )}
      {session?.kind === 'vod' && (
        <RecordingPlayer
          key={session.streamUrl}
          fileName=""
          streamUrl={session.streamUrl}
          title={session.title}
          durationSec={session.durationSec}
          modeLabel="VOD"
          mode={mode}
          onClose={value.close}
          onMinimize={value.minimize}
          onExpand={value.expand}
        />
      )}
      {session?.kind === 'embed' && (
        <EmbedPlayer
          key={session.embedUrl}
          embedUrl={session.embedUrl}
          title={session.title}
          channelName={session.channelName}
          mode={mode}
          onClose={value.close}
          onMinimize={value.minimize}
          onExpand={value.expand}
        />
      )}
    </PlayerContext.Provider>
  )
}

export function usePlayer() {
  const ctx = useContext(PlayerContext)
  if (!ctx) throw new Error('usePlayer must be used within PlayerProvider')
  return ctx
}
