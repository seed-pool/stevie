const LANG_TO_COUNTRY: Record<string, string> = {
  eng: 'gb',
  en: 'gb',
  ara: 'sa',
  ar: 'sa',
  cze: 'cz',
  ces: 'cz',
  cs: 'cz',
  dan: 'dk',
  da: 'dk',
  ger: 'de',
  deu: 'de',
  de: 'de',
  gre: 'gr',
  ell: 'gr',
  el: 'gr',
  fre: 'fr',
  fra: 'fr',
  fr: 'fr',
  spa: 'es',
  es: 'es',
  ita: 'it',
  it: 'it',
  jpn: 'jp',
  ja: 'jp',
  kor: 'kr',
  ko: 'kr',
  chi: 'cn',
  zho: 'cn',
  zh: 'cn',
  por: 'pt',
  pt: 'pt',
  rus: 'ru',
  ru: 'ru',
  swe: 'se',
  sv: 'se',
  nor: 'no',
  no: 'no',
  fin: 'fi',
  fi: 'fi',
  pol: 'pl',
  pl: 'pl',
  hun: 'hu',
  hu: 'hu',
  dut: 'nl',
  nld: 'nl',
  nl: 'nl',
  tur: 'tr',
  tr: 'tr',
  heb: 'il',
  he: 'il',
  hin: 'in',
  hi: 'in',
  tha: 'th',
  th: 'th',
  vie: 'vn',
  vi: 'vn',
  ukr: 'ua',
  uk: 'ua',
  ron: 'ro',
  rum: 'ro',
  ro: 'ro',
  bul: 'bg',
  bg: 'bg',
  hrv: 'hr',
  hr: 'hr',
  srp: 'rs',
  sr: 'rs',
  slk: 'sk',
  slo: 'sk',
  sk: 'sk',
  slv: 'si',
  sl: 'si',
  lit: 'lt',
  lt: 'lt',
  lav: 'lv',
  lv: 'lv',
  est: 'ee',
  et: 'ee',
  ind: 'id',
  id: 'id',
  may: 'my',
  msa: 'my',
  ms: 'my',
  cat: 'es',
  ca: 'es',
  ice: 'is',
  isl: 'is',
  is: 'is',
}

export function countryForLang(lang?: string | null): string | null {
  if (!lang) return null
  return LANG_TO_COUNTRY[lang.toLowerCase()] ?? null
}

export function LangFlag({ lang, label }: { lang?: string | null; label?: string }) {
  const code = countryForLang(lang)
  const text = (label || lang || 'und').toUpperCase()
  if (!code) {
    return <span className="lang-pill">{text}</span>
  }
  return (
    <span className="lang-pill" title={text}>
      <img
        className="lang-flag"
        src={`https://flagcdn.com/w40/${code}.png`}
        srcSet={`https://flagcdn.com/w80/${code}.png 2x`}
        width={20}
        height={15}
        alt=""
        loading="lazy"
      />
      <span>{text}</span>
    </span>
  )
}
