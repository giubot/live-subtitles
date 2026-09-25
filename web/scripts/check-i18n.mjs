#!/usr/bin/env node
// SPDX-License-Identifier: Apache-2.0
// Fail when locales disagree: every language must have the same namespaces,
// the same keys in each, and no empty strings (UI-3). Languages are the
// folders under src/locales, so a new UI language is checked as soon as its
// folder exists (UI-8).
//
// Plural keys (`count_one`, `count_other`, …) are compared by their base
// name, because languages have different plural forms. Each language must
// have `_other` plus every form its CLDR rules pick for counts 0–1000, and
// may add any other form its rules know (e.g. pt `_many`, for millions).
// Run: pnpm check:i18n
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'

const dir = new URL('../src/locales/', import.meta.url).pathname

function flatten(obj, prefix = '') {
  return Object.entries(obj).flatMap(([k, v]) =>
    v && typeof v === 'object' ? flatten(v, `${prefix}${k}.`) : [[`${prefix}${k}`, v]],
  )
}

const langs = readdirSync(dir, { withFileTypes: true })
  .filter((d) => d.isDirectory())
  .map((d) => d.name)
  .sort()
const keys = {} // lang → ns → Map(key → value)
for (const lang of langs) {
  keys[lang] = {}
  for (const file of readdirSync(join(dir, lang)).filter((f) => f.endsWith('.json'))) {
    const ns = file.slice(0, -'.json'.length)
    keys[lang][ns] = new Map(flatten(JSON.parse(readFileSync(join(dir, lang, file), 'utf8'))))
  }
}

const pluralForms = ['zero', 'one', 'two', 'few', 'many', 'other']
const pluralRe = new RegExp(`^(.*)_(${pluralForms.join('|')})$`)

/** The plural forms a language knows, and those it needs for everyday counts. */
function pluralRules(lang) {
  const rules = new Intl.PluralRules(lang)
  const known = new Set(rules.resolvedOptions().pluralCategories)
  const needed = new Set(['other'])
  for (let n = 0; n <= 1000; n++) needed.add(rules.select(n))
  return { known, needed }
}

/** Key → base: `x_one` becomes `x` when the language also has `x_other`. */
function canonical(have) {
  const out = new Map()
  for (const key of have.keys()) {
    const m = pluralRe.exec(key)
    const plural = m && have.has(`${m[1]}_other`)
    const base = plural ? m[1] : key
    if (!out.has(base)) out.set(base, { plural: Boolean(plural), forms: new Set() })
    if (plural) out.get(base).forms.add(m[2])
  }
  return out
}

const problems = []
const namespaces = [...new Set(langs.flatMap((l) => Object.keys(keys[l])))].sort()
for (const ns of namespaces) {
  const bases = Object.fromEntries(langs.map((l) => [l, canonical(keys[l][ns] ?? new Map())]))
  const all = [...new Set(langs.flatMap((l) => [...bases[l].keys()]))].sort()
  const pluralBases = new Set(all.filter((b) => langs.some((l) => bases[l].get(b)?.plural)))
  for (const lang of langs) {
    const have = keys[lang][ns]
    if (!have) {
      problems.push(`${lang}/${ns}.json is missing`)
      continue
    }
    for (const [key, value] of have)
      if (typeof value !== 'string' || value.trim() === '')
        problems.push(`${lang}/${ns}.json: "${key}" must be a non-empty string`)
    const { known, needed } = pluralRules(lang)
    for (const base of all) {
      const entry = bases[lang].get(base)
      if (!entry) {
        const shown = pluralBases.has(base) ? `${base}_other` : base
        problems.push(`${lang}/${ns}.json: missing key "${shown}"`)
      } else if (pluralBases.has(base) && !entry.plural) {
        problems.push(`${lang}/${ns}.json: "${base}" must be plural (${base}_one, ${base}_other…)`)
      } else if (entry.plural) {
        for (const form of needed)
          if (!entry.forms.has(form))
            problems.push(`${lang}/${ns}.json: missing key "${base}_${form}"`)
        for (const form of entry.forms)
          if (!known.has(form))
            problems.push(`${lang}/${ns}.json: "${base}_${form}" isn't a plural form of ${lang}`)
      }
    }
  }
}

if (problems.length) {
  console.error(problems.join('\n'))
  console.error(`\n${problems.length} i18n problem(s) across ${langs.join(', ')}`)
  process.exit(1)
}
console.log(`i18n ok: ${langs.join(', ')} × ${namespaces.length} namespaces`)
