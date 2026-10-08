#!/usr/bin/env node
/**
 * Download league marks into apps/server/assets/team-logos/leagues/
 * and write leagues.json (merged into the team-logo pack).
 */
import { mkdirSync, writeFileSync, existsSync, readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const OUT = join(__dirname, '..', 'assets', 'team-logos', 'leagues')
const ESPN = 'https://site.api.espn.com/apis/site/v2/sports'
const UA = 'Mozilla/5.0 (compatible; StevieSports/1.0)'

/**
 * Direct CDN league marks (preferred — stable paths).
 * `cdn` is the filename under leagues/500 or espn/teamlogos/500.
 * @type {{ key: string, name: string, sport: string, aliases: string[], cdn?: string, cdnAlt?: string }[]}
 */
const STATIC = [
  // Big-4 / NA
  { key: 'nhl', name: 'NHL', sport: 'Ice Hockey', aliases: ['National Hockey League', 'nhl'] },
  { key: 'nba', name: 'NBA', sport: 'Basketball', aliases: ['National Basketball Association', 'nba'] },
  { key: 'mlb', name: 'MLB', sport: 'Baseball', aliases: ['Major League Baseball', 'mlb'] },
  { key: 'nfl', name: 'NFL', sport: 'American Football', aliases: ['National Football League', 'nfl'] },
  { key: 'wnba', name: 'WNBA', sport: 'Basketball', aliases: ["Women's National Basketball Association", 'wnba'] },
  { key: 'mls', name: 'MLS', sport: 'Football', aliases: ['Major League Soccer', 'mls', 'usa.1'] },
  // CFL mark (ESPN CDN 404 — SportsDB badge)
  {
    key: 'cfl',
    name: 'CFL',
    sport: 'American Football',
    aliases: ['Canadian Football League', 'cfl'],
    cdnAlt: 'https://r2.thesportsdb.com/images/media/league/badge/ffypv51488739128.png',
  },
  // Motorsport
  { key: 'f1', name: 'Formula 1', sport: 'MotorSport', aliases: ['F1', 'Formula One', 'Formel 1', 'formula 1', 'formel 1'] },
  { key: 'nascar', name: 'NASCAR', sport: 'MotorSport', aliases: ['NASCAR Cup Series', 'nascar cup', 'nascar'], cdnAlt: 'espn:nascar' },
  // Fighting
  { key: 'ufc', name: 'UFC', sport: 'Fighting', aliases: ['Ultimate Fighting Championship', 'ufc', 'UFC Fight Night'] },
  { key: 'pfl', name: 'PFL', sport: 'Fighting', aliases: ['Professional Fighters League', 'pfl'] },
  { key: 'boxing', name: 'Boxing', sport: 'Fighting', aliases: ['boxing', 'boks', 'prizefighting'], cdnAlt: 'espn:boxing' },
  // Golf
  { key: 'pgatour', name: 'PGA Tour', sport: 'Golf', aliases: ['PGA', 'PGA Tour', 'pgatour', 'pga tour'] },
  { key: 'lpga', name: 'LPGA', sport: 'Golf', aliases: ['Ladies Professional Golf Association', 'LPGA Tour', 'lpga'] },
  { key: 'european_tour', name: 'DP World Tour', sport: 'Golf', aliases: ['European Tour', 'DP World Tour', 'dp world', 'eur'], cdnAlt: 'espn:european_tour' },
  { key: 'pga_champions_tour', name: 'PGA Tour Champions', sport: 'Golf', aliases: ['Champions Tour', 'PGA Champions', 'pga tour champions'], cdnAlt: 'espn:pga_champions_tour' },
]

/** Soccer (and others) — pull logo href from ESPN scoreboard when CDN key differs. */
const SCOREBOARDS = [
  { sportPath: 'soccer', leaguePath: 'eng.1', key: 'eng.1', name: 'English Premier League', sport: 'Football', aliases: ['Premier League', 'EPL', 'eng.1'] },
  { sportPath: 'soccer', leaguePath: 'usa.1', key: 'usa.1', name: 'MLS', sport: 'Football', aliases: ['Major League Soccer', 'usa.1'] },
  { sportPath: 'soccer', leaguePath: 'uefa.champions', key: 'uefa.champions', name: 'UEFA Champions League', sport: 'Football', aliases: ['Champions League', 'UCL'] },
  { sportPath: 'soccer', leaguePath: 'esp.1', key: 'esp.1', name: 'Spanish La Liga', sport: 'Football', aliases: ['La Liga', 'LaLiga', 'esp.1'] },
  { sportPath: 'soccer', leaguePath: 'ita.1', key: 'ita.1', name: 'Italian Serie A', sport: 'Football', aliases: ['Serie A', 'ita.1'] },
  { sportPath: 'soccer', leaguePath: 'ger.1', key: 'ger.1', name: 'German Bundesliga', sport: 'Football', aliases: ['Bundesliga', 'ger.1'] },
  { sportPath: 'soccer', leaguePath: 'fra.1', key: 'fra.1', name: 'French Ligue 1', sport: 'Football', aliases: ['Ligue 1', 'fra.1'] },
  // Extra scoreboard pulls (logos often only on the league object).
  { sportPath: 'racing', leaguePath: 'f1', key: 'f1', name: 'Formula 1', sport: 'MotorSport', aliases: ['F1', 'Formula One', 'Formel 1'] },
  { sportPath: 'mma', leaguePath: 'ufc', key: 'ufc', name: 'UFC', sport: 'Fighting', aliases: ['Ultimate Fighting Championship', 'ufc'] },
  { sportPath: 'mma', leaguePath: 'pfl', key: 'pfl', name: 'PFL', sport: 'Fighting', aliases: ['Professional Fighters League', 'pfl'] },
  { sportPath: 'golf', leaguePath: 'pga', key: 'pgatour', name: 'PGA Tour', sport: 'Golf', aliases: ['PGA', 'PGA Tour'] },
  { sportPath: 'golf', leaguePath: 'lpga', key: 'lpga', name: 'LPGA', sport: 'Golf', aliases: ['LPGA', 'LPGA Tour'] },
  { sportPath: 'golf', leaguePath: 'eur', key: 'european_tour', name: 'DP World Tour', sport: 'Golf', aliases: ['European Tour', 'DP World Tour'] },
  { sportPath: 'golf', leaguePath: 'champions-tour', key: 'pga_champions_tour', name: 'PGA Tour Champions', sport: 'Golf', aliases: ['Champions Tour', 'PGA Champions'] },
  { sportPath: 'racing', leaguePath: 'nascar-premier', key: 'nascar', name: 'NASCAR', sport: 'MotorSport', aliases: ['NASCAR Cup Series', 'nascar'] },
]

function cdnCandidates(key, cdnAlt) {
  const out = [
    `https://a.espncdn.com/i/teamlogos/leagues/500/${key}.png`,
    `https://a.espncdn.com/i/espn/teamlogos/500/${key}.png`,
  ]
  if (cdnAlt?.startsWith('espn:')) {
    out.unshift(`https://a.espncdn.com/i/espn/teamlogos/500/${cdnAlt.slice(5)}.png`)
  } else if (cdnAlt) {
    out.unshift(cdnAlt)
  }
  return out
}

async function download(url, dest) {
  if (existsSync(dest) && readFileSync(dest).length > 500) return true
  const res = await fetch(url, { headers: { 'User-Agent': UA, Accept: 'image/*' } })
  if (!res.ok) return false
  const buf = Buffer.from(await res.arrayBuffer())
  if (buf.length < 500) return false
  // Skip tiny ESPN placeholder icons (generic sport glyphs).
  if (buf.length < 2500 && /ESPN-icon/i.test(url)) return false
  mkdirSync(dirname(dest), { recursive: true })
  writeFileSync(dest, buf)
  return true
}

async function downloadFirst(urls, dest) {
  for (const url of urls) {
    if (await download(url, dest)) return url
  }
  return ''
}

async function getJSON(url) {
  const res = await fetch(url, { headers: { 'User-Agent': UA, Accept: 'application/json' } })
  if (!res.ok) throw new Error(`${res.status} ${url}`)
  return res.json()
}

function pushLeague(leagues, entry) {
  const existing = leagues.find((x) => x.key === entry.key)
  if (existing) {
    for (const a of entry.aliases || []) {
      if (!existing.aliases.includes(a)) existing.aliases.push(a)
    }
    return
  }
  leagues.push(entry)
}

mkdirSync(OUT, { recursive: true })
/** @type {any[]} */
const leagues = []

for (const lg of STATIC) {
  const file = `${lg.key}.png`
  const dest = join(OUT, file)
  const okUrl = await downloadFirst(cdnCandidates(lg.key, lg.cdnAlt), dest)
  if (!okUrl) {
    console.warn('fail static', lg.key)
    continue
  }
  pushLeague(leagues, {
    key: `leagues/${lg.key}`,
    file: `leagues/${file}`,
    name: lg.name,
    sport: lg.sport,
    aliases: lg.aliases,
  })
  console.log('ok', lg.key, okUrl.includes('/espn/') ? '(espn cdn)' : '')
}

for (const lg of SCOREBOARDS) {
  const file = `${lg.key}.png`
  const dest = join(OUT, file)
  let href = ''
  try {
    const data = await getJSON(`${ESPN}/${lg.sportPath}/${lg.leaguePath}/scoreboard`)
    const logos = data?.leagues?.[0]?.logos || []
    const dark = logos.find((l) => (l.rel || []).includes('dark'))
    const full = logos.find((l) => (l.rel || []).includes('full') || (l.rel || []).includes('default'))
    href = (dark || full || logos[0])?.href || ''
    // Skip generic ESPN icon glyphs.
    if (/ESPN-icon/i.test(href)) href = ''
  } catch (err) {
    console.warn('scoreboard', lg.key, err.message || err)
  }
  if (!href && existsSync(dest) && readFileSync(dest).length > 500) {
    // Already have a CDN mark from STATIC.
    pushLeague(leagues, {
      key: `leagues/${lg.key}`,
      file: `leagues/${file}`,
      name: lg.name,
      sport: lg.sport,
      aliases: lg.aliases,
    })
    continue
  }
  if (!href) {
    // Try CDN fallbacks for scoreboard-only entries.
    href = await downloadFirst(cdnCandidates(lg.key), dest)
    if (!href) {
      console.warn('fail', lg.key)
      continue
    }
  } else {
    const ok = await download(href, dest)
    if (!ok) {
      console.warn('fail', lg.key, href)
      continue
    }
  }
  if (lg.key === 'usa.1' && leagues.some((x) => x.key === 'leagues/mls')) {
    const mls = leagues.find((x) => x.key === 'leagues/mls')
    for (const a of lg.aliases) {
      if (!mls.aliases.includes(a)) mls.aliases.push(a)
    }
    continue
  }
  pushLeague(leagues, {
    key: `leagues/${lg.key}`,
    file: `leagues/${file}`,
    name: lg.name,
    sport: lg.sport,
    aliases: lg.aliases,
  })
  console.log('ok', lg.key)
}

writeFileSync(
  join(dirname(OUT), 'leagues.json'),
  JSON.stringify({ version: 1, generated_at: new Date().toISOString(), leagues }, null, 2),
)
console.log(JSON.stringify({ leagues: leagues.length, out: OUT }))
