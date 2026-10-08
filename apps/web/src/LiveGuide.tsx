import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
} from 'react'
import { Link } from 'react-router-dom'
import {
  api,
  LIVE_FAVORITES_GROUP,
  liveLogoUrl,
  notifyLiveRecordingChange,
  type LiveCategory,
  type LiveGuideChannel,
  type LiveProgram,
  type ScheduledRecording,
} from './api'
import {
  CategoryDrawer,
  CategoryDrawerChip,
  CategoryOptionList,
  useCategoryDrawer,
} from './CategoryDrawer'
import { CategoryDropdown } from './CategoryMenu'
import { sortCategoriesWithPins, useCategoryPins } from './categoryPins'
import { Icon, icons } from './icons'
import { usePlayer } from './PlayerContext'
import { useTimePrefs } from './TimePrefsContext'
import { useColumnResize } from './useColumnResize'

type SlotMenu = {
  channelId: string
  channelName: string
  program: LiveProgram
  x: number
  y: number
}

type FuturePicker = {
  channelId: string
  channelName: string
  title: string
  x: number
  y: number
  /** Prefill from the tapped programme when available. */
  startLocal?: string
  endLocal?: string
}

const DOW = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa']

function pad2(n: number) {
  return String(n).padStart(2, '0')
}

function toLocalDateValue(d: Date) {
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
}

function toLocalTimeValue(d: Date) {
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

function localPartsToDate(date: string, time: string) {
  const [y, m, day] = date.split('-').map(Number)
  const [hh, mm] = time.split(':').map(Number)
  if (![y, m, day, hh, mm].every((n) => Number.isFinite(n))) return null
  return new Date(y, m - 1, day, hh, mm, 0, 0)
}

function monthLabel(year: number, month: number) {
  return new Date(year, month, 1).toLocaleString([], { month: 'long', year: 'numeric' })
}

function buildMonthCells(year: number, month: number) {
  const first = new Date(year, month, 1)
  const startPad = first.getDay()
  const daysInMonth = new Date(year, month + 1, 0).getDate()
  const cells: { date: Date; inMonth: boolean }[] = []
  for (let i = 0; i < startPad; i++) {
    const d = new Date(year, month, i - startPad + 1)
    cells.push({ date: d, inMonth: false })
  }
  for (let day = 1; day <= daysInMonth; day++) {
    cells.push({ date: new Date(year, month, day), inMonth: true })
  }
  while (cells.length % 7 !== 0) {
    const last = cells[cells.length - 1].date
    cells.push({ date: new Date(last.getFullYear(), last.getMonth(), last.getDate() + 1), inMonth: false })
  }
  return cells
}

const ROW_H = 64
const MIN_CHANNEL_W = 120
const MAX_CHANNEL_W = 360
const MIN_HOUR_PX = 80
const MAX_HOUR_PX = 480
const DEFAULT_CHANNEL_W = 200
const DEFAULT_HOUR_PX = 240
const VISIBLE_BUFFER = 8
const EPG_BATCH = 100
const LOAD_MORE_ROWS = 12

function loadNum(key: string, fallback: number) {
  try {
    const v = Number(localStorage.getItem(key))
    return Number.isFinite(v) && v > 0 ? v : fallback
  } catch {
    return fallback
  }
}

function ms(iso: string) {
  return new Date(iso).getTime()
}


function programStyle(p: LiveProgram, fromMs: number, toMs: number, hourPx: number) {
  const start = Math.max(ms(p.start_time), fromMs)
  const end = Math.min(ms(p.end_time), toMs)
  const left = ((start - fromMs) / 3_600_000) * hourPx
  const width = Math.max(((end - start) / 3_600_000) * hourPx, 8)
  return { left, width }
}

function nowLeft(fromMs: number, hourPx: number) {
  return ((Date.now() - fromMs) / 3_600_000) * hourPx
}

function useDebounced(value: string, msDelay = 250) {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(value.trim()), msDelay)
    return () => window.clearTimeout(t)
  }, [value, msDelay])
  return debounced
}

export function LiveGuide() {
  const { playLive } = usePlayer()
  const { formatHour } = useTimePrefs()
  const [group, setGroup] = useState('')
  const [total, setTotal] = useState(0)
  const [groups, setGroups] = useState<string[]>([])
  const [categories, setCategories] = useState<LiveCategory[]>([])
  const [favoritesCount, setFavoritesCount] = useState(0)
  const [channels, setChannels] = useState<LiveGuideChannel[]>([])
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [now, setNow] = useState(Date.now())
  const [channelQuery, setChannelQuery] = useState('')
  const [programQuery, setProgramQuery] = useState('')
  const debouncedChannelQ = useDebounced(channelQuery)
  const debouncedProgramQ = useDebounced(programQuery)
  const [channelW, setChannelW] = useState(() => loadNum('stevie.epg.channelW', DEFAULT_CHANNEL_W))
  const [hourPx, setHourPx] = useState(() => loadNum('stevie.epg.hourPx', DEFAULT_HOUR_PX))
  const [scrollTop, setScrollTop] = useState(0)
  const [viewportH, setViewportH] = useState(600)
  const [slotMenu, setSlotMenu] = useState<SlotMenu | null>(null)
  const [futurePicker, setFuturePicker] = useState<FuturePicker | null>(null)
  const [scheduling, setScheduling] = useState(false)
  const [scheduled, setScheduled] = useState<ScheduledRecording[]>([])
  const [pickTitle, setPickTitle] = useState('')
  const [pickDate, setPickDate] = useState('')
  const [pickStart, setPickStart] = useState('20:00')
  const [pickEnd, setPickEnd] = useState('21:00')
  const [calMonth, setCalMonth] = useState(() => {
    const d = new Date()
    return { year: d.getFullYear(), month: d.getMonth() }
  })
  const bodyRef = useRef<HTMLDivElement>(null)
  const mobileListRef = useRef<HTMLDivElement>(null)
  const prevSearching = useRef(false)
  const loadMoreLock = useRef(false)
  const searching = !!(debouncedChannelQ || debouncedProgramQ)
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
  } = useCategoryDrawer('live')

  const refreshScheduled = useCallback(() => {
    api
      .listScheduledRecordings()
      .then((r) => setScheduled(r.scheduled ?? []))
      .catch(() => setScheduled([]))
  }, [])

  useEffect(() => {
    refreshScheduled()
    const onChange = () => refreshScheduled()
    window.addEventListener('stevie:live-recordings', onChange)
    return () => window.removeEventListener('stevie:live-recordings', onChange)
  }, [refreshScheduled])

  useEffect(() => {
    if (!slotMenu && !futurePicker) return
    const close = () => {
      setSlotMenu(null)
      setFuturePicker(null)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close()
    }
    window.addEventListener('pointerdown', close)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('pointerdown', close)
      window.removeEventListener('keydown', onKey)
    }
  }, [slotMenu, futurePicker])

  const scheduledKeys = useMemo(() => {
    const keys = new Set<string>()
    for (const s of scheduled) {
      if (s.status !== 'scheduled' && s.status !== 'starting' && s.status !== 'recording') continue
      keys.add(`${s.channel_id}|${s.start_time}|${s.end_time}`)
      if (s.program_id) keys.add(s.program_id)
    }
    return keys
  }, [scheduled])

  const isSlotScheduled = useCallback(
    (channelId: string, p: LiveProgram) =>
      scheduledKeys.has(p.id) || scheduledKeys.has(`${channelId}|${p.start_time}|${p.end_time}`),
    [scheduledKeys],
  )

  const placePopup = useCallback((clientX: number, clientY: number, w: number, h: number) => {
    const pad = 8
    let x = clientX
    let y = clientY
    if (x + w + pad > window.innerWidth) x = window.innerWidth - w - pad
    if (y + h + pad > window.innerHeight) y = window.innerHeight - h - pad
    if (x < pad) x = pad
    if (y < pad) y = pad
    return { x, y }
  }, [])

  const onProgramClick = useCallback(
    (e: ReactMouseEvent, ch: LiveGuideChannel, p: LiveProgram) => {
      const start = ms(p.start_time)
      const end = ms(p.end_time)
      const live = start <= now && now < end
      if (live || end <= now) {
        playLive(ch.id, ch.name)
        return
      }
      e.stopPropagation()
      const { x, y } = placePopup(e.clientX, e.clientY, 210, 96)
      setFuturePicker(null)
      setSlotMenu({ channelId: ch.id, channelName: ch.name, program: p, x, y })
    },
    [now, playLive, placePopup],
  )

  const openFuturePicker = useCallback(() => {
    if (!slotMenu) return
    const start = new Date(slotMenu.program.start_time)
    const end = new Date(slotMenu.program.end_time)
    const { x, y } = placePopup(slotMenu.x, slotMenu.y, 320, 420)
    setPickTitle(slotMenu.program.title || slotMenu.channelName)
    setPickDate(toLocalDateValue(start))
    setPickStart(toLocalTimeValue(start))
    setPickEnd(toLocalTimeValue(end))
    setCalMonth({ year: start.getFullYear(), month: start.getMonth() })
    setSlotMenu(null)
    setFuturePicker({
      channelId: slotMenu.channelId,
      channelName: slotMenu.channelName,
      title: slotMenu.program.title || slotMenu.channelName,
      x,
      y,
      startLocal: toLocalDateValue(start),
      endLocal: toLocalDateValue(end),
    })
  }, [slotMenu, placePopup])

  const recordSlot = useCallback(async () => {
    if (!slotMenu || scheduling) return
    setScheduling(true)
    setError('')
    try {
      await api.scheduleRecording({
        channel_id: slotMenu.channelId,
        program_id: slotMenu.program.id,
        title: slotMenu.program.title,
        description: slotMenu.program.description,
        category: slotMenu.program.category,
        start_time: slotMenu.program.start_time,
        end_time: slotMenu.program.end_time,
      })
      setSlotMenu(null)
      notifyLiveRecordingChange()
      refreshScheduled()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not schedule recording')
    } finally {
      setScheduling(false)
    }
  }, [slotMenu, scheduling, refreshScheduled])

  const recordFuture = useCallback(async () => {
    if (!futurePicker || scheduling) return
    const start = localPartsToDate(pickDate, pickStart)
    const end = localPartsToDate(pickDate, pickEnd)
    if (!start || !end) {
      setError('Pick a valid date and time')
      return
    }
    // If end is earlier/equal on the same calendar day, treat as next-day end.
    if (end <= start) {
      end.setDate(end.getDate() + 1)
    }
    if (end <= new Date()) {
      setError('End time must be in the future')
      return
    }
    setScheduling(true)
    setError('')
    try {
      await api.scheduleRecording({
        channel_id: futurePicker.channelId,
        title: pickTitle.trim() || futurePicker.channelName,
        description: `Manual schedule · ${futurePicker.channelName}`,
        start_time: start.toISOString(),
        end_time: end.toISOString(),
      })
      setFuturePicker(null)
      notifyLiveRecordingChange()
      refreshScheduled()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not schedule recording')
    } finally {
      setScheduling(false)
    }
  }, [futurePicker, scheduling, pickDate, pickStart, pickEnd, pickTitle, refreshScheduled])

  const calCells = useMemo(
    () => buildMonthCells(calMonth.year, calMonth.month),
    [calMonth.year, calMonth.month],
  )
  const todayKey = toLocalDateValue(new Date())
  const minMonth = useMemo(() => {
    const d = new Date()
    return { year: d.getFullYear(), month: d.getMonth() }
  }, [now])
  const canPrevMonth =
    calMonth.year > minMonth.year || (calMonth.year === minMonth.year && calMonth.month > minMonth.month)

  const channelDrag = useColumnResize(channelW, setChannelW, MIN_CHANNEL_W, MAX_CHANNEL_W, 'stevie.epg.channelW')
  const hourDrag = useColumnResize(hourPx, setHourPx, MIN_HOUR_PX, MAX_HOUR_PX, 'stevie.epg.hourPx')

  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 30_000)
    return () => window.clearInterval(t)
  }, [])

  // Starting a search defaults to All categories; dropdown stays usable afterward.
  useEffect(() => {
    if (searching && !prevSearching.current) {
      setGroup('')
    }
    prevSearching.current = searching
  }, [searching])

  // Initial / filter-change load (first batch).
  useEffect(() => {
    const ac = new AbortController()
    loadMoreLock.current = false
    setLoading(true)
    setLoadingMore(false)
    setError('')
    setChannels([])
    setTotal(0)
    setScrollTop(0)
    if (bodyRef.current) bodyRef.current.scrollTop = 0

    api
      .liveGuide({
        group: group || undefined,
        q: debouncedChannelQ || undefined,
        pq: debouncedProgramQ || undefined,
        limit: EPG_BATCH,
        offset: 0,
        signal: ac.signal,
      })
      .then((g) => {
        if (ac.signal.aborted) return
        setGroups(g.groups ?? [])
        setCategories(g.categories ?? [])
        setChannels(g.channels ?? [])
        setTotal(g.total ?? g.channels?.length ?? 0)
        setFrom(g.from)
        setTo(g.to)
        setFavoritesCount(g.favorites_count ?? 0)
      })
      .catch((err) => {
        if (ac.signal.aborted || (err instanceof DOMException && err.name === 'AbortError')) return
        if (err instanceof Error && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : 'Failed to load guide')
      })
      .finally(() => {
        if (!ac.signal.aborted) setLoading(false)
      })
    return () => ac.abort()
  }, [group, debouncedChannelQ, debouncedProgramQ])

  const hasMore = channels.length < total

  const loadMore = useCallback(() => {
    if (loading || loadingMore || loadMoreLock.current || !hasMore) return
    loadMoreLock.current = true
    setLoadingMore(true)
    const offset = channels.length
    api
      .liveGuide({
        group: group || undefined,
        q: debouncedChannelQ || undefined,
        pq: debouncedProgramQ || undefined,
        limit: EPG_BATCH,
        offset,
      })
      .then((g) => {
        setGroups(g.groups ?? [])
        setCategories(g.categories ?? [])
        setTotal(g.total ?? 0)
        setFrom(g.from)
        setTo(g.to)
        setFavoritesCount(g.favorites_count ?? 0)
        const next = g.channels ?? []
        setChannels((prev) => {
          const seen = new Set(prev.map((c) => c.id))
          const appended = next.filter((c) => !seen.has(c.id))
          return appended.length ? [...prev, ...appended] : prev
        })
      })
      .catch((err) => {
        if (err instanceof Error && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : 'Failed to load more channels')
      })
      .finally(() => {
        loadMoreLock.current = false
        setLoadingMore(false)
      })
  }, [
    loading,
    loadingMore,
    hasMore,
    channels.length,
    group,
    debouncedChannelQ,
    debouncedProgramQ,
  ])

  useEffect(() => {
    const el = bodyRef.current
    if (!el) return
    const onScroll = () => setScrollTop(el.scrollTop)
    const ro = new ResizeObserver(() => setViewportH(el.clientHeight || 600))
    el.addEventListener('scroll', onScroll, { passive: true })
    ro.observe(el)
    setViewportH(el.clientHeight || 600)
    return () => {
      el.removeEventListener('scroll', onScroll)
      ro.disconnect()
    }
  }, [channels.length])

  // Autoload next batch when the virtual window nears the end of loaded rows.
  useEffect(() => {
    if (!hasMore || loading || loadingMore) return
    const nearEnd =
      scrollTop + viewportH >= channels.length * ROW_H - LOAD_MORE_ROWS * ROW_H
    if (nearEnd) loadMore()
  }, [scrollTop, viewportH, channels.length, hasMore, loading, loadingMore, loadMore])

  const toggleFavorite = useCallback(async (ch: LiveGuideChannel) => {
    const next = !ch.favorite
    setChannels((prev) => prev.map((c) => (c.id === ch.id ? { ...c, favorite: next } : c)))
    try {
      const r = await api.setLiveFavorite(ch.id, next)
      setFavoritesCount(r.favorites_count)
      setChannels((prev) =>
        prev.map((c) => (c.id === ch.id ? { ...c, favorite: r.channel.favorite } : c)),
      )
      // If viewing Favorites and unfavorited, drop the row.
      if (group === LIVE_FAVORITES_GROUP && !r.channel.favorite) {
        setChannels((prev) => prev.filter((c) => c.id !== ch.id))
      }
    } catch (err) {
      setChannels((prev) => prev.map((c) => (c.id === ch.id ? { ...c, favorite: ch.favorite } : c)))
      setError(err instanceof Error ? err.message : 'Failed to update favorite')
    }
  }, [group])

  const fromMs = from ? ms(from) : Date.now()
  const toMs = to ? ms(to) : fromMs + 6 * 3_600_000
  const hours = useMemo(() => {
    const out: Date[] = []
    const start = new Date(fromMs)
    start.setMinutes(0, 0, 0)
    let t = start.getTime()
    if (t < fromMs) t += 3_600_000
    for (; t < toMs; t += 3_600_000) out.push(new Date(t))
    return out
  }, [fromMs, toMs])
  const timelineW = Math.max(((toMs - fromMs) / 3_600_000) * hourPx, hourPx)
  const playhead = nowLeft(fromMs, hourPx)

  const totalH = channels.length * ROW_H
  const startIdx = Math.max(0, Math.floor(scrollTop / ROW_H) - VISIBLE_BUFFER)
  const visibleCount = Math.ceil(viewportH / ROW_H) + VISIBLE_BUFFER * 2
  const endIdx = Math.min(channels.length, startIdx + visibleCount)
  const visible = channels.slice(startIdx, endIdx)

  const { pinned, toggle: togglePin } = useCategoryPins('live')

  const categoryOptions = useMemo(() => {
    const rest = categories.length
      ? categories.map((c) => ({
          value: c.name,
          label: c.name,
          count: c.channel_count,
          pinable: true as const,
        }))
      : groups.map((g) => ({
          value: g,
          label: g || 'Ungrouped',
          pinable: true as const,
        }))
    const sorted = sortCategoriesWithPins(
      rest,
      pinned,
      (c) => c.value,
      (c) => c.label,
    )
    return [
      {
        value: LIVE_FAVORITES_GROUP,
        label: 'Favorites',
        count: favoritesCount,
        pinable: false,
      },
      ...sorted,
    ]
  }, [categories, groups, favoritesCount, pinned])

  const scopeHint =
    group === LIVE_FAVORITES_GROUP
      ? searching
        ? 'searching Favorites'
        : 'Favorites'
      : group
        ? searching
          ? `searching in ${group}`
          : `in ${group}`
        : searching
          ? 'searching all categories'
          : 'All categories'

  const selectedCatLabel =
    group === LIVE_FAVORITES_GROUP
      ? 'Favorites'
      : group
        ? categoryOptions.find((c) => c.value === group)?.label || group
        : 'All categories'

  const pickGroup = (v: string) => {
    setGroup(v)
    if (isMobile) closeCats()
  }

  return (
    <div
      className={[
        'live-page',
        isMobile ? 'is-mobile' : '',
        catsOpen ? 'cats-open' : '',
        catsDragging ? 'cats-dragging' : '',
      ]
        .filter(Boolean)
        .join(' ')}
    >
      {isMobile && (
        <CategoryDrawer
          title="Live categories"
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
            value={group}
            allLabel="All categories"
            options={categoryOptions}
            pinned={pinned}
            onChange={pickGroup}
            onTogglePin={togglePin}
          />
        </CategoryDrawer>
      )}

      <div className="section-head">
        <h1>
          <Icon icon={icons.live} className="section-icon" /> Live TV
        </h1>
        {isMobile && <CategoryDrawerChip label={selectedCatLabel} onClick={openCats} />}
        <span className="muted">
          {total
            ? `${channels.length} of ${total} channels`
            : `${channels.length} channels shown`}
          {scopeHint ? ` · ${scopeHint}` : ''}
          {loadingMore ? ' · loading…' : ''}
        </span>
      </div>

      <div className="live-toolbar">
        {!isMobile && (
          <CategoryDropdown
            label="Category"
            value={group}
            allLabel="All categories"
            options={categoryOptions}
            pinned={pinned}
            onChange={(v) => setGroup(v)}
            onTogglePin={togglePin}
          />
        )}
        <label>
          Channels
          <input
            value={channelQuery}
            onChange={(e) => setChannelQuery(e.target.value)}
            placeholder={group ? 'Search channels…' : 'Search all channels…'}
            enterKeyHint="search"
          />
        </label>
        <label>
          Programmes
          <input
            value={programQuery}
            onChange={(e) => setProgramQuery(e.target.value)}
            placeholder={group ? 'Search programmes…' : 'Search all programmes…'}
            enterKeyHint="search"
          />
        </label>
        <p className="muted live-toolbar-hint desktop-only">
          Browse All categories or pick one to narrow. Scroll to load more channels. Star channels for Favorites.
          Sources in <Link to="/settings">Settings</Link>.
        </p>
      </div>

      {error && <div className="error">{error}</div>}
      {loading && <p className="muted">Loading guide…</p>}

      {!loading && !channels.length && (
        <div className="status-card" style={{ padding: '1.2rem' }}>
          <p>
            {searching
              ? 'No channels match your search.'
              : group === LIVE_FAVORITES_GROUP
                ? 'No favorites yet — star channels to add them here.'
                : 'No live channels yet.'}
          </p>
          {!searching && group !== LIVE_FAVORITES_GROUP && (
            <>
              <p className="muted">
                Import Xtream categories or add an M3U under Settings → Live TV.
              </p>
              <Link to="/settings">Open Settings</Link>
            </>
          )}
        </div>
      )}

      {!!channels.length && isMobile && (
        <div
          className="epg-mobile"
          ref={mobileListRef}
          onScroll={() => {
            const el = mobileListRef.current
            if (!el || !hasMore || loading || loadingMore) return
            if (el.scrollTop + el.clientHeight >= el.scrollHeight - 240) void loadMore()
          }}
        >
          {channels.map((ch) => {
            const logoSrc = liveLogoUrl(ch.logo_url)
            const current =
              ch.programs.find((p) => ms(p.start_time) <= now && now < ms(p.end_time)) || null
            const next = !current
              ? ch.programs.find((p) => ms(p.start_time) > now) || null
              : ch.programs.find((p) => ms(p.start_time) >= ms(current.end_time)) || null
            const prog = current || next
            return (
              <div key={ch.id} className="epg-mobile-row">
                <button
                  type="button"
                  className={ch.favorite ? 'epg-fav is-on' : 'epg-fav'}
                  title={ch.favorite ? 'Remove from Favorites' : 'Add to Favorites'}
                  aria-label={ch.favorite ? 'Remove from Favorites' : 'Add to Favorites'}
                  aria-pressed={!!ch.favorite}
                  onClick={() => void toggleFavorite(ch)}
                >
                  <Icon icon={icons.star} />
                </button>
                <button
                  type="button"
                  className="epg-mobile-main"
                  onClick={() => playLive(ch.id, ch.name)}
                >
                  {logoSrc ? (
                    <img
                      src={logoSrc}
                      alt=""
                      loading="lazy"
                      onError={(e) => {
                        e.currentTarget.style.display = 'none'
                        const fallback = e.currentTarget.nextElementSibling
                        if (fallback instanceof HTMLElement) fallback.hidden = false
                      }}
                    />
                  ) : null}
                  <span className="epg-logo-fallback" hidden={!!logoSrc}>
                    <Icon icon={icons.live} />
                  </span>
                  <span className="epg-mobile-meta">
                    <strong>{ch.name}</strong>
                    {prog ? (
                      <span className="epg-mobile-prog">
                        {current ? <span className="epg-mobile-live">LIVE</span> : null}
                        <span className="epg-mobile-title">{prog.title}</span>
                        <span className="muted">
                          {formatHour(new Date(prog.start_time))} – {formatHour(new Date(prog.end_time))}
                        </span>
                      </span>
                    ) : (
                      <span className="muted">No schedule · tap to play</span>
                    )}
                  </span>
                </button>
                {prog && ms(prog.start_time) > now ? (
                  <button
                    type="button"
                    className="ghost epg-mobile-rec"
                    title="Schedule recording"
                    aria-label={`Schedule ${prog.title}`}
                    onClick={(e) => onProgramClick(e, ch, prog)}
                  >
                    <Icon icon={icons.record} />
                  </button>
                ) : null}
              </div>
            )
          })}
          {(loadingMore || hasMore) && (
            <div className="epg-load-more muted">
              {loadingMore ? 'Loading more…' : hasMore ? 'Scroll for more' : null}
            </div>
          )}
        </div>
      )}

      {!!channels.length && !isMobile && (
        <div className="epg">
          <div className="epg-scroll" ref={bodyRef}>
            <div className="epg-inner" style={{ width: channelW + timelineW }}>
              <div className="epg-header">
                <div className="epg-corner" style={{ width: channelW }}>
                  <span className="muted">Channels</span>
                  <div
                    className="epg-col-resizer"
                    title="Drag to resize channel column"
                    onPointerDown={channelDrag.onPointerDown}
                    onPointerMove={channelDrag.onPointerMove}
                    onPointerUp={channelDrag.onPointerUp}
                    onPointerCancel={channelDrag.onPointerUp}
                  />
                </div>
                <div className="epg-hours" style={{ width: timelineW }}>
                  {hours.map((h) => {
                    const left = ((h.getTime() - fromMs) / 3_600_000) * hourPx
                    return (
                      <div
                        key={h.toISOString()}
                        className="epg-hour"
                        style={{
                          left,
                          width: hourPx,
                        }}
                      >
                        <span>{formatHour(h)}</span>
                        <div
                          className="epg-col-resizer"
                          title="Drag to resize time columns"
                          onPointerDown={hourDrag.onPointerDown}
                          onPointerMove={hourDrag.onPointerMove}
                          onPointerUp={hourDrag.onPointerUp}
                          onPointerCancel={hourDrag.onPointerUp}
                        />
                      </div>
                    )
                  })}
                </div>
              </div>

              <div className="epg-body" style={{ height: totalH, position: 'relative' }}>
                {playhead >= 0 && playhead <= timelineW && (
                  <div className="epg-now" style={{ left: channelW + playhead }} />
                )}
                <div style={{ height: startIdx * ROW_H }} />
                {visible.map((ch) => {
                  const logoSrc = liveLogoUrl(ch.logo_url)
                  return (
                  <div key={ch.id} className="epg-row" style={{ height: ROW_H }}>
                    <div className="epg-channel" style={{ width: channelW, height: ROW_H }}>
                      <button
                        type="button"
                        className={ch.favorite ? 'epg-fav is-on' : 'epg-fav'}
                        title={ch.favorite ? 'Remove from Favorites' : 'Add to Favorites'}
                        aria-label={ch.favorite ? 'Remove from Favorites' : 'Add to Favorites'}
                        aria-pressed={!!ch.favorite}
                        onClick={(e) => {
                          e.stopPropagation()
                          void toggleFavorite(ch)
                        }}
                      >
                        <Icon icon={icons.star} />
                      </button>
                      <button
                        type="button"
                        className="epg-channel-main"
                        title={`Play ${ch.name}`}
                        onClick={() => playLive(ch.id, ch.name)}
                      >
                        {logoSrc ? (
                          <img
                            src={logoSrc}
                            alt=""
                            loading="lazy"
                            onError={(e) => {
                              e.currentTarget.style.display = 'none'
                              const fallback = e.currentTarget.nextElementSibling
                              if (fallback instanceof HTMLElement) fallback.hidden = false
                            }}
                          />
                        ) : null}
                        <span className="epg-logo-fallback" hidden={!!logoSrc}>
                          <Icon icon={icons.live} />
                        </span>
                        <span className="epg-channel-meta">
                          <strong>{ch.name}</strong>
                          <small className="muted">{ch.group_title}</small>
                        </span>
                      </button>
                    </div>
                    <div className="epg-programs" style={{ width: timelineW, height: ROW_H }}>
                      {ch.programs.map((p) => {
                        const { left, width } = programStyle(p, fromMs, toMs, hourPx)
                        const live = ms(p.start_time) <= now && now < ms(p.end_time)
                        const future = ms(p.start_time) > now
                        const booked = isSlotScheduled(ch.id, p)
                        const match =
                          debouncedProgramQ &&
                          `${p.title} ${p.description}`.toLowerCase().includes(debouncedProgramQ.toLowerCase())
                        return (
                          <button
                            key={p.id}
                            type="button"
                            className={[
                              'epg-program',
                              live ? 'is-live' : '',
                              match ? 'is-match' : '',
                              booked ? 'is-scheduled' : '',
                              future ? 'is-future' : '',
                            ]
                              .filter(Boolean)
                              .join(' ')}
                            style={{ left, width, height: ROW_H - 10 }}
                            title={
                              booked
                                ? `Scheduled · ${p.title}`
                                : future
                                  ? `Schedule or view · ${p.description || p.title}`
                                  : p.description || p.title
                            }
                            onClick={(e) => onProgramClick(e, ch, p)}
                          >
                            <strong>{p.title}</strong>
                            <small>
                              {formatHour(new Date(p.start_time))} – {formatHour(new Date(p.end_time))}
                              {booked ? ' · REC' : ''}
                            </small>
                          </button>
                        )
                      })}
                      {!ch.programs.length && (
                        <button
                          type="button"
                          className="epg-program is-empty"
                          style={{ left: 0, width: timelineW, height: ROW_H - 10 }}
                          onClick={() => playLive(ch.id, ch.name)}
                        >
                          <strong>No schedule</strong>
                          <small>Tap to play channel</small>
                        </button>
                      )}
                    </div>
                  </div>
                  )
                })}
                <div style={{ height: Math.max(0, (channels.length - endIdx) * ROW_H) }} />
              </div>
            </div>
            {(loadingMore || hasMore) && (
              <div className="epg-load-more muted">
                {loadingMore ? 'Loading more channels…' : hasMore ? 'Scroll for more' : null}
              </div>
            )}
          </div>
        </div>
      )}
      {slotMenu && (
        <div
          className="epg-slot-menu"
          style={{ left: slotMenu.x, top: slotMenu.y }}
          role="menu"
          onPointerDown={(e) => e.stopPropagation()}
        >
          <button
            type="button"
            role="menuitem"
            disabled={scheduling || isSlotScheduled(slotMenu.channelId, slotMenu.program)}
            onClick={() => void recordSlot()}
          >
            {isSlotScheduled(slotMenu.channelId, slotMenu.program)
              ? 'Already scheduled'
              : scheduling
                ? 'Scheduling…'
                : 'Record this Slot'}
          </button>
          <button type="button" role="menuitem" disabled={scheduling} onClick={openFuturePicker}>
            Record Future Slot
          </button>
        </div>
      )}
      {futurePicker && (
        <div
          className="epg-future-picker"
          style={{ left: futurePicker.x, top: futurePicker.y }}
          role="dialog"
          aria-label="Schedule future recording"
          onPointerDown={(e) => e.stopPropagation()}
        >
          <div className="epg-future-picker-head">
            <strong>Record Future Slot</strong>
            <span className="muted">{futurePicker.channelName}</span>
          </div>
          <div className="epg-cal-nav">
            <button
              type="button"
              aria-label="Previous month"
              disabled={!canPrevMonth}
              onClick={() =>
                setCalMonth((m) =>
                  m.month === 0 ? { year: m.year - 1, month: 11 } : { year: m.year, month: m.month - 1 },
                )
              }
            >
              ‹
            </button>
            <strong>{monthLabel(calMonth.year, calMonth.month)}</strong>
            <button
              type="button"
              aria-label="Next month"
              onClick={() =>
                setCalMonth((m) =>
                  m.month === 11 ? { year: m.year + 1, month: 0 } : { year: m.year, month: m.month + 1 },
                )
              }
            >
              ›
            </button>
          </div>
          <div className="epg-cal-grid" aria-label="Calendar">
            {DOW.map((d) => (
              <div key={d} className="epg-cal-dow">
                {d}
              </div>
            ))}
            {calCells.map(({ date, inMonth }) => {
              const key = toLocalDateValue(date)
              const past = key < todayKey
              return (
                <button
                  key={key + (inMonth ? '' : '-o')}
                  type="button"
                  className={[
                    'epg-cal-day',
                    inMonth ? '' : 'is-outside',
                    key === todayKey ? 'is-today' : '',
                    key === pickDate ? 'is-selected' : '',
                  ]
                    .filter(Boolean)
                    .join(' ')}
                  disabled={past}
                  onClick={() => setPickDate(key)}
                >
                  {date.getDate()}
                </button>
              )
            })}
          </div>
          <div className="epg-future-fields">
            <label>
              Title
              <input
                value={pickTitle}
                onChange={(e) => setPickTitle(e.target.value)}
                placeholder={futurePicker.channelName}
              />
            </label>
            <div className="epg-future-times">
              <label>
                Start
                <input type="time" value={pickStart} onChange={(e) => setPickStart(e.target.value)} />
              </label>
              <label>
                End
                <input type="time" value={pickEnd} onChange={(e) => setPickEnd(e.target.value)} />
              </label>
            </div>
          </div>
          <div className="epg-future-actions">
            <button type="button" className="ghost" onClick={() => setFuturePicker(null)}>
              Cancel
            </button>
            <button
              type="button"
              className="primary"
              disabled={scheduling || !pickDate}
              onClick={() => void recordFuture()}
            >
              {scheduling ? 'Scheduling…' : 'Schedule'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
