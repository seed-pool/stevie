import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  api,
  LIVE_FAVORITES_GROUP,
  liveLogoUrl,
  notifyLiveRecordingChange,
  sportsBadgeUrl,
  type SportsChannel,
  type SportsEvent,
} from './api'
import {
  CategoryDrawer,
  CategoryDrawerChip,
  CategoryOptionList,
  useCategoryDrawer,
} from './CategoryDrawer'
import { CategoryDropdown, type CategoryMenuOption } from './CategoryMenu'
import { sortCategoriesWithPins, useCategoryPins } from './categoryPins'
import { Icon, sportIcon } from './icons'
import { usePlayer } from './PlayerContext'
import { useTimePrefs } from './TimePrefsContext'

type ChannelMenu = {
  event: SportsEvent
  x: number
  y: number
}

/** Pregame window — matches server Live detection (broadcast before tip-off). */
const PREGAME_MS = 30 * 60 * 1000

const EXTERNAL_SPORT = 'External (streamed.pk)'
const SPORT_SELECTION_KEY = 'stevie.sports.selectedSport'

function readStoredSport(): string {
  try {
    return localStorage.getItem(SPORT_SELECTION_KEY) ?? ''
  } catch {
    return ''
  }
}

function writeStoredSport(value: string) {
  try {
    localStorage.setItem(SPORT_SELECTION_KEY, value)
  } catch {
    /* ignore */
  }
}

const SPORT_ORDER = [
  'Basketball',
  'Baseball',
  'Ice Hockey',
  'American Football',
  'Football',
  EXTERNAL_SPORT,
]

const EXTERNAL_CAT_ORDER = [
  'Basketball',
  'Baseball',
  'Hockey',
  'American Football',
  'Football',
  'Fight',
  'Motor Sports',
  'Tennis',
  'Golf',
  'Rugby',
  'Cricket',
  'Other',
]

function liveEndGraceMs(sport: string) {
  // Matches server sports API live window.
  return sport === 'Baseball' ? 90 * 60 * 1000 : 0
}

function recordingEndPadMs(sport: string) {
  if (sport === 'Baseball') return 90 * 60 * 1000
  if (sport === 'Ice Hockey' || sport === 'Basketball') return 45 * 60 * 1000
  return 30 * 60 * 1000
}

function eventTiming(ev: SportsEvent, nowMs = Date.now()) {
  const start = new Date(ev.starts_at).getTime()
  const end = new Date(ev.ends_at).getTime()
  if (Number.isNaN(start) || Number.isNaN(end)) {
    return { live: !!ev.live, upcoming: !!ev.upcoming }
  }
  const airStart = start - PREGAME_MS
  const grace = liveEndGraceMs(ev.sport)
  const live = nowMs >= airStart && nowMs < end + grace
  const upcoming = nowMs < airStart
  return { live, upcoming }
}

/** Build a schedule window the recordings API will accept for live / overrun games. */
function recordingWindow(ev: SportsEvent, nowMs = Date.now()) {
  const tip = new Date(ev.starts_at).getTime()
  const listedEnd = new Date(ev.ends_at).getTime()
  const fallbackTip = Number.isNaN(tip) ? nowMs : tip
  const pad = recordingEndPadMs(ev.sport)
  const minRemaining = 45 * 60 * 1000
  const defaultLen = 3 * 60 * 60 * 1000

  let startMs = Number.isNaN(tip) ? nowMs : tip - PREGAME_MS
  if (startMs < nowMs) startMs = nowMs

  let endMs = Number.isNaN(listedEnd) ? fallbackTip + defaultLen : listedEnd + pad
  endMs = Math.max(endMs, fallbackTip + defaultLen, nowMs + minRemaining)
  if (endMs <= startMs) endMs = startMs + minRemaining

  return {
    start_time: new Date(startMs).toISOString(),
    end_time: new Date(endMs).toISOString(),
  }
}

function placeMenu(clientX: number, clientY: number) {
  const pad = 12
  const w = 280
  const h = 220
  let x = clientX
  let y = clientY
  if (x + w > window.innerWidth - pad) x = window.innerWidth - w - pad
  if (y + h > window.innerHeight - pad) y = window.innerHeight - h - pad
  if (x < pad) x = pad
  if (y < pad) y = pad
  return { x, y }
}

function sortEvents(a: SportsEvent, b: SportsEvent) {
  const ta = eventTiming(a)
  const tb = eventTiming(b)
  if (ta.live !== tb.live) return ta.live ? -1 : 1
  if (ta.upcoming !== tb.upcoming) return ta.upcoming ? -1 : 1
  return new Date(a.starts_at).getTime() - new Date(b.starts_at).getTime()
}

function groupBySport(events: SportsEvent[]) {
  const map = new Map<string, SportsEvent[]>()
  for (const ev of events) {
    const key = ev.sport || 'Other'
    const list = map.get(key)
    if (list) list.push(ev)
    else map.set(key, [ev])
  }
  for (const list of map.values()) list.sort(sortEvents)

  const keys = [...map.keys()].sort((a, b) => {
    const liveA = (map.get(a) ?? []).some((e) => eventTiming(e).live)
    const liveB = (map.get(b) ?? []).some((e) => eventTiming(e).live)
    if (liveA !== liveB) return liveA ? -1 : 1
    const ia = SPORT_ORDER.indexOf(a)
    const ib = SPORT_ORDER.indexOf(b)
    if (ia !== ib) {
      if (ia < 0) return 1
      if (ib < 0) return -1
      return ia - ib
    }
    return a.localeCompare(b)
  })
  return keys.map((sport) => ({ sport, events: map.get(sport) ?? [] }))
}

/** Within External (streamed.pk), group by upstream category (football, fight, …). */
function groupByStreamedCategory(events: SportsEvent[]) {
  const map = new Map<string, SportsEvent[]>()
  for (const ev of events) {
    const key = (ev.streamed_category || 'Other').trim() || 'Other'
    const list = map.get(key)
    if (list) list.push(ev)
    else map.set(key, [ev])
  }
  for (const list of map.values()) list.sort(sortEvents)
  const keys = [...map.keys()].sort((a, b) => {
    const liveA = (map.get(a) ?? []).some((e) => eventTiming(e).live)
    const liveB = (map.get(b) ?? []).some((e) => eventTiming(e).live)
    if (liveA !== liveB) return liveA ? -1 : 1
    const ia = EXTERNAL_CAT_ORDER.indexOf(a)
    const ib = EXTERNAL_CAT_ORDER.indexOf(b)
    if (ia !== ib) {
      if (ia < 0) return 1
      if (ib < 0) return -1
      return ia - ib
    }
    return a.localeCompare(b)
  })
  return keys.map((sport) => ({ sport, events: map.get(sport) ?? [] }))
}

function TeamBadge({ name, badgeUrl }: { name: string; badgeUrl?: string }) {
  const src = sportsBadgeUrl(badgeUrl)
  const [failed, setFailed] = useState(false)
  const initials = name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase() ?? '')
    .join('')
  useEffect(() => {
    setFailed(false)
  }, [src])
  return (
    <div className="sports-badge" title={name}>
      {src && !failed ? (
        <img src={src} alt="" loading="lazy" onError={() => setFailed(true)} />
      ) : (
        <span className="sports-badge-fallback">{initials || '?'}</span>
      )}
    </div>
  )
}

function SportSectionTitle({ sport }: { sport: string }) {
  return (
    <h2 className="sports-sport-heading">
      <Icon icon={sportIcon(sport)} className="sports-sport-icon" />
      <span>{sport}</span>
    </h2>
  )
}

export function SportsPage() {
  const { playLive, playEmbed, playStreamed } = usePlayer()
  const { formatWhen } = useTimePrefs()
  const { pinned, toggle } = useCategoryPins('sports')
  const {
    isMobile,
    open: catsOpen,
    dragging: catsDragging,
    drawerRef: catsDrawerRef,
    drawerStyle: catsDrawerStyle,
    handleStyle: catsHandleStyle,
    backdropStyle: catsBackdropStyle,
    openDrawer: openCats,
    close: closeCats,
  } = useCategoryDrawer('sports')

  const [sport, setSport] = useState(readStoredSport)
  const [liveOnly, setLiveOnly] = useState(false)
  const [metaSports, setMetaSports] = useState<{ sport: string; count: number }[]>([])
  const [events, setEvents] = useState<SportsEvent[]>([])
  const [hotEvents, setHotEvents] = useState<SportsEvent[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [menu, setMenu] = useState<ChannelMenu | null>(null)
  const [scheduling, setScheduling] = useState(false)
  const [hint, setHint] = useState('')

  const isFavorites = sport === LIVE_FAVORITES_GROUP
  const isExternal = sport === EXTERNAL_SPORT
  const isMultiSport = !sport || isFavorites

  useEffect(() => {
    writeStoredSport(sport)
  }, [sport])

  const load = useCallback(async (opts?: { quiet?: boolean }) => {
    if (!opts?.quiet) setError('')
    try {
      const [meta, list] = await Promise.all([
        api.sportsMeta(),
        isFavorites
          ? api.sportsEvents({ favorites: pinned })
          : sport
            ? api.sportsEvents({ sport })
            : api.sportsEvents(),
      ])
      setMetaSports(meta.sports ?? [])
      setEvents(list.events ?? [])
      setHotEvents(list.hot ?? [])
    } catch (err) {
      if (!opts?.quiet) {
        setError(err instanceof Error ? err.message : 'Failed to load sports')
      }
    } finally {
      setLoading(false)
    }
  }, [sport, pinned, isFavorites])

  const hasLive = useMemo(
    () => events.some((ev) => eventTiming(ev).live),
    [events],
  )

  useEffect(() => {
    setLoading(true)
    void load()
  }, [load])

  useEffect(() => {
    const ms = hasLive ? 15_000 : 60_000
    const t = window.setInterval(() => void load({ quiet: true }), ms)
    return () => window.clearInterval(t)
  }, [load, hasLive])

  useEffect(() => {
    if (isFavorites && pinned.length === 0) setSport('')
  }, [isFavorites, pinned.length])

  useEffect(() => {
    if (!menu) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMenu(null)
    }
    const onDoc = (e: MouseEvent) => {
      const el = document.querySelector('.sports-channel-menu')
      if (el && !el.contains(e.target as Node)) setMenu(null)
    }
    document.addEventListener('keydown', onKey)
    document.addEventListener('mousedown', onDoc)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('mousedown', onDoc)
    }
  }, [menu])

  const options: CategoryMenuOption[] = useMemo(() => {
    const sorted = sortCategoriesWithPins(
      metaSports,
      pinned,
      (s) => s.sport,
      (s) => s.sport,
    )
    const rest = sorted.map((s) => ({
      value: s.sport,
      label: s.sport,
      count: s.count,
    }))
    if (!pinned.length) return rest
    const favCount = metaSports
      .filter((s) => pinned.includes(s.sport))
      .reduce((n, s) => n + s.count, 0)
    return [
      {
        value: LIVE_FAVORITES_GROUP,
        label: 'Favorites',
        count: favCount || undefined,
        pinable: false,
      },
      ...rest,
    ]
  }, [metaSports, pinned])

  const selectedCatLabel = isFavorites ? 'Favorites' : sport || 'All sports'

  const visibleEvents = useMemo(
    () => (liveOnly ? events.filter((e) => eventTiming(e).live) : events),
    [events, liveOnly],
  )

  const visibleHot = useMemo(() => {
    if (isExternal || isFavorites) return []
    return hotEvents.filter((e) => eventTiming(e).live).slice(0, 8)
  }, [hotEvents, isExternal, isFavorites])

  /** Home / favorites: group by sport. External: by streamed.pk category. Else Live / Upcoming. */
  const bySport = useMemo(
    () => (isMultiSport ? groupBySport(visibleEvents) : []),
    [visibleEvents, isMultiSport],
  )
  const byExternalCat = useMemo(
    () => (isExternal ? groupByStreamedCategory(visibleEvents) : []),
    [visibleEvents, isExternal],
  )
  const liveEvents = useMemo(
    () =>
      sport && !isExternal && !isFavorites
        ? visibleEvents.filter((e) => eventTiming(e).live).sort(sortEvents)
        : [],
    [visibleEvents, sport, isExternal, isFavorites],
  )
  const upcomingEvents = useMemo(
    () =>
      sport && !isExternal && !isFavorites
        ? visibleEvents.filter((e) => eventTiming(e).upcoming).sort(sortEvents)
        : [],
    [visibleEvents, sport, isExternal, isFavorites],
  )
  const otherEvents = useMemo(
    () =>
      sport && !isExternal && !isFavorites
        ? visibleEvents
            .filter((e) => !eventTiming(e).live && !eventTiming(e).upcoming)
            .sort(sortEvents)
        : [],
    [visibleEvents, sport, isExternal, isFavorites],
  )

  const liveFilterControl = (
    <label
      className={['sports-live-filter', liveOnly ? 'is-on' : ''].filter(Boolean).join(' ')}
      title={liveOnly ? 'Showing live events only' : 'Show live events only'}
    >
      <input
        type="checkbox"
        checked={liveOnly}
        onChange={(e) => setLiveOnly(e.target.checked)}
        aria-label="Show live events only"
      />
      <span className="sports-live-pill sports-live-filter-pill">Live</span>
    </label>
  )

  const openMenu = (ev: SportsEvent, clientX: number, clientY: number) => {
    const { x, y } = placeMenu(clientX, clientY)
    setMenu({ event: ev, x, y })
    setHint('')
  }

  const scheduleChannel = async (ev: SportsEvent, ch: SportsChannel) => {
    if (scheduling) return
    const isStreamed = ch.source === 'streamed' && !!(ch.streamed_source && ch.streamed_id)
    if (isStreamed) {
      if (ev.upcoming || !ch.streamed_source || !ch.streamed_id) return
      setScheduling(true)
      setHint('')
      try {
        await api.startStreamedRecording({
          source: ch.streamed_source,
          id: ch.streamed_id,
          stream: ch.streamed_no || 1,
          program_title: sportsMatchupTitle(ev),
          channel_name: ch.name || 'Streamed',
        })
        notifyLiveRecordingChange()
        setHint('Recording started')
        setMenu(null)
      } catch (err) {
        setHint(err instanceof Error ? err.message : 'Could not record')
      } finally {
        setScheduling(false)
      }
      return
    }
    if (!ch.playable || !ch.id) return
    setScheduling(true)
    setHint('')
    try {
      const window = recordingWindow(ev)
      await api.scheduleRecording({
        channel_id: ch.id,
        title: sportsMatchupTitle(ev),
        description: `Sports · ${ch.broadcast_label || ch.name || ''}`,
        category: ev.sport,
        start_time: window.start_time,
        end_time: window.end_time,
      })
      notifyLiveRecordingChange()
      setHint('Scheduled')
      setMenu(null)
    } catch (err) {
      setHint(err instanceof Error ? err.message : 'Could not schedule')
    } finally {
      setScheduling(false)
    }
  }

  const onSelectSport = (value: string) => {
    setSport(value)
    closeCats()
  }

  const pageClass = [
    'live-page',
    'sports-page',
    isMobile ? 'is-mobile' : '',
    catsOpen ? 'cats-open' : '',
    catsDragging ? 'cats-dragging' : '',
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <div className={pageClass}>
      {isMobile && (
        <CategoryDrawer
          title="Sports"
          open={catsOpen}
          dragging={catsDragging}
          drawerRef={catsDrawerRef}
          drawerStyle={catsDrawerStyle}
          handleStyle={catsHandleStyle}
          backdropStyle={catsBackdropStyle}
          onOpen={openCats}
          onClose={closeCats}
        >
          <CategoryOptionList
            value={sport}
            allLabel="All sports"
            allCount={metaSports.reduce((n, s) => n + s.count, 0) || undefined}
            options={options}
            pinned={pinned}
            onChange={onSelectSport}
            onTogglePin={toggle}
          />
        </CategoryDrawer>
      )}

      <div className="section-head sports-section-head">
        {isMobile && <CategoryDrawerChip label={selectedCatLabel} onClick={openCats} />}
        <h1>Sports</h1>
        <span className="muted sports-head-sub">
          {isFavorites
            ? 'Favorite sports'
            : sport
              ? sport
              : 'Live scores and upcoming games'}
        </span>
        {isMobile && liveFilterControl}
      </div>

      {!isMobile && (
        <div className="live-toolbar sports-toolbar">
          <CategoryDropdown
            label="Sport"
            value={sport}
            allLabel="All sports"
            options={options}
            pinned={pinned}
            onChange={setSport}
            onTogglePin={toggle}
          />
          {liveFilterControl}
        </div>
      )}

      {error && <p className="error">{error}</p>}
      {hint && <p className="muted">{hint}</p>}
      {loading && <p className="muted">Loading…</p>}

      {!loading && events.length === 0 && (
        <p className="muted">
          No live or upcoming games in the next 7 days. Sync runs automatically in the background.
        </p>
      )}

      {!loading && events.length > 0 && visibleEvents.length === 0 && liveOnly && (
        <p className="muted">No live events right now. Turn off Live only to see upcoming games.</p>
      )}

      {!loading && !isExternal && visibleHot.length > 0 && (
        <section className="sports-section sports-hot-section">
          <h2 className="sports-hot-heading">Hot</h2>
          <div className="sports-grid">
            {visibleHot.map((ev) => (
              <MatchCard key={`hot-${ev.id}`} event={ev} onOpen={openMenu} formatWhen={formatWhen} />
            ))}
          </div>
        </section>
      )}

      {isMultiSport &&
        bySport.map((group) => (
          <section key={group.sport} className="sports-section">
            <SportSectionTitle sport={group.sport} />
            <div className="sports-grid">
              {group.events.map((ev) => (
                <MatchCard key={ev.id} event={ev} onOpen={openMenu} formatWhen={formatWhen} />
              ))}
            </div>
          </section>
        ))}

      {isExternal &&
        byExternalCat.map((group) => (
          <section key={group.sport} className="sports-section">
            <SportSectionTitle sport={group.sport} />
            <div className="sports-grid">
              {group.events.map((ev) => (
                <MatchCard key={ev.id} event={ev} onOpen={openMenu} formatWhen={formatWhen} />
              ))}
            </div>
          </section>
        ))}

      {sport && !isExternal && !isFavorites && liveEvents.length > 0 && (
        <section className="sports-section">
          <h2>Live now</h2>
          <div className="sports-grid">
            {liveEvents.map((ev) => (
              <MatchCard key={ev.id} event={ev} onOpen={openMenu} formatWhen={formatWhen} />
            ))}
          </div>
        </section>
      )}

      {sport && !isExternal && !isFavorites && upcomingEvents.length > 0 && (
        <section className="sports-section">
          <h2>Upcoming</h2>
          <div className="sports-grid">
            {upcomingEvents.map((ev) => (
              <MatchCard key={ev.id} event={ev} onOpen={openMenu} formatWhen={formatWhen} />
            ))}
          </div>
        </section>
      )}

      {sport && !isExternal && !isFavorites && otherEvents.length > 0 && (
        <section className="sports-section">
          <h2>Recently started</h2>
          <div className="sports-grid">
            {otherEvents.map((ev) => (
              <MatchCard key={ev.id} event={ev} onOpen={openMenu} formatWhen={formatWhen} />
            ))}
          </div>
        </section>
      )}

      {menu && (
        <div
          className="sports-channel-menu epg-slot-menu"
          style={{ left: menu.x, top: menu.y }}
          role="menu"
        >
          <div className="sports-channel-menu-head">
            <strong>{menu.event.title}</strong>
            <span className="muted">{formatWhen(menu.event.starts_at, { date: true })}</span>
          </div>
          {menu.event.channels.length === 0 && (
            <p className="muted sports-channel-empty">No channels listed</p>
          )}
          {[...menu.event.channels]
            .sort((a, b) => {
              const aStreamed = a.source === 'streamed'
              const bStreamed = b.source === 'streamed'
              if (aStreamed !== bStreamed) return aStreamed ? 1 : -1
              if (!!a.confirmed !== !!b.confirmed) return a.confirmed ? -1 : 1
              if (a.playable !== b.playable) return a.playable ? -1 : 1
              return (a.name || a.broadcast_label).localeCompare(b.name || b.broadcast_label)
            })
            .map((ch) => {
            const logo = liveLogoUrl(ch.logo_url)
            const isStreamed =
              ch.source === 'streamed' && !!(ch.streamed_source && ch.streamed_id)
            const canPlay = isStreamed || (!!ch.playable && !!ch.id)
            return (
              <div
                key={`${ch.source || 'iptv'}-${ch.broadcast_label}-${ch.id || ch.embed_url || 'x'}`}
                className="sports-channel-row"
              >
                <div className="sports-channel-meta">
                  {logo ? <img src={logo} alt="" className="sports-channel-logo" /> : null}
                  <div>
                    <div>
                      {ch.name || ch.broadcast_label}
                      {ch.confirmed ? (
                        <span className="muted sports-channel-sub"> · listed</span>
                      ) : null}
                    </div>
                    {isStreamed ? (
                      <div className="muted sports-channel-sub">streamed.pk</div>
                    ) : ch.name && ch.broadcast_label && ch.name !== ch.broadcast_label ? (
                      <div className="muted sports-channel-sub">{ch.broadcast_label}</div>
                    ) : null}
                    {!ch.playable && !isStreamed && (
                      <div className="muted sports-channel-sub">Not in library</div>
                    )}
                  </div>
                </div>
                <div className="sports-channel-actions">
                  <button
                    type="button"
                    disabled={!canPlay}
                    onClick={() => {
                      if (isStreamed && ch.streamed_source && ch.streamed_id) {
                        playStreamed(
                          {
                            source: ch.streamed_source,
                            id: ch.streamed_id,
                            stream: ch.streamed_no || 1,
                          },
                          sportsMatchupTitle(menu.event),
                          ch.name || 'Streamed',
                        )
                        setMenu(null)
                        return
                      }
                      if (ch.embed_url) {
                        playEmbed(ch.embed_url, sportsMatchupTitle(menu.event), ch.name || 'Streamed')
                        setMenu(null)
                        return
                      }
                      if (ch.id) {
                        playLive(ch.id, sportsMatchupTitle(menu.event))
                        setMenu(null)
                      }
                    }}
                  >
                    Play
                  </button>
                  <button
                    type="button"
                    disabled={
                      scheduling ||
                      (isStreamed
                        ? !ch.streamed_source || !ch.streamed_id || !!menu.event.upcoming
                        : !ch.playable || !ch.id)
                    }
                    title={
                      isStreamed
                        ? menu.event.upcoming
                          ? 'Scheduling not available for streamed.pk yet'
                          : 'Start recording (stop from the player)'
                        : menu.event.upcoming
                          ? 'Schedule recording'
                          : 'Record / schedule'
                    }
                    onClick={() => void scheduleChannel(menu.event, ch)}
                  >
                    {menu.event.upcoming ? 'Schedule' : 'Record'}
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

/** Prefer Away @ Home for overlays / recording names over generic EPG titles. */
function sportsMatchupTitle(ev: SportsEvent): string {
  const away = (ev.away?.name || '').trim()
  const home = (ev.home?.name || '').trim()
  if (away && home) return `${away} @ ${home}`
  return (ev.title || home || away || 'Sports').trim()
}

/** Keep live clock text; drop ESPN schedule dates like "10/7 - 6:00 PM EDT". */
function sportsPeriodLabel(period?: string) {
  const p = (period || '').trim()
  if (!p) return ''
  if (/\d{1,2}\/\d{1,2}/.test(p)) return ''
  if (/^\d{1,2}:\d{2}\s*[AP]M/i.test(p) && !/\b(Q|quarter|half|inning|period|top|bot|end|final|ot|so)\b/i.test(p)) {
    return ''
  }
  return p
}

function MatchCard({
  event,
  onOpen,
  formatWhen,
}: {
  event: SportsEvent
  onOpen: (ev: SportsEvent, x: number, y: number) => void
  formatWhen: (iso: string, opts?: { date?: boolean }) => string
}) {
  const home = event.home.name || 'Home'
  const away = event.away.name || ''
  const playable = event.channels.some((c) => c.playable)
  const { live } = eventTiming(event)
  const hasScore = !!(event.score && (event.score.home || event.score.away))
  const playableCount = event.channels.filter((c) => c.playable).length
  // ESPN puts calendar crumbs like "10/7 - 6:00 PM EDT" in period for scheduled games —
  // only show real clock text (Q3, Halftime, Top 3rd, Final…).
  const period = sportsPeriodLabel(event.score?.period)
  const leagueMark = sportsBadgeUrl(event.league_logo_url)

  return (
    <button
      type="button"
      className={[
        'sports-card',
        live ? 'is-live' : '',
        playable ? '' : 'is-unmatched',
        leagueMark ? 'has-league-mark' : '',
      ]
        .filter(Boolean)
        .join(' ')}
      onClick={(e) => onOpen(event, e.clientX, e.clientY)}
    >
      {leagueMark ? (
        <span
          className="sports-card-league-mark"
          style={{ backgroundImage: `url("${leagueMark}")` }}
          aria-hidden="true"
        />
      ) : null}
      {live && <span className="sports-live-pill">Live</span>}

      <div className="sports-card-matchup">
        {away ? (
          <div className="sports-team-row">
            <TeamBadge name={away} badgeUrl={event.away.badge_url} />
            <span className="sports-card-name">{away}</span>
            {hasScore && <span className="sports-score">{event.score?.away || '0'}</span>}
          </div>
        ) : null}
        <div className="sports-team-row">
          <TeamBadge name={home} badgeUrl={event.home.badge_url} />
          <span className="sports-card-name">{home}</span>
          {hasScore && <span className="sports-score">{event.score?.home || '0'}</span>}
        </div>
      </div>

      <div className="sports-card-foot">
        <span className="muted">
          {event.streamed_category ? `${event.streamed_category} · ` : ''}
          {period ? `${period} · ` : ''}
          {formatWhen(event.starts_at, { date: true })}
        </span>
        <span className="muted">
          {playableCount}/{event.channels.length || 0} ch
        </span>
      </div>
    </button>
  )
}
