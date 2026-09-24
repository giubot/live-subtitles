#!/usr/bin/env node
// SPDX-License-Identifier: Apache-2.0
// Fail when locales disagree: every language must have the same namespaces,
// the same keys in each, and no empty strings (UI-3).
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

const problems = []
const namespaces = [...new Set(langs.flatMap((l) => Object.keys(keys[l])))].sort()
for (const ns of namespaces) {
  const all = [...new Set(langs.flatMap((l) => [...(keys[l][ns]?.keys() ?? [])]))].sort()
  for (const lang of langs) {
    const have = keys[lang][ns]
    if (!have) {
      problems.push(`${lang}/${ns}.json is missing`)
      continue
    }
    for (const key of all) {
      if (!have.has(key)) problems.push(`${lang}/${ns}.json: missing key "${key}"`)
      else if (typeof have.get(key) !== 'string' || have.get(key).trim() === '')
        problems.push(`${lang}/${ns}.json: "${key}" must be a non-empty string`)
    }
  }
}

if (problems.length) {
  console.error(problems.join('\n'))
  console.error(`\n${problems.length} i18n problem(s) across ${langs.join(', ')}`)
  process.exit(1)
}
console.log(`i18n ok: ${langs.join(', ')} × ${namespaces.length} namespaces`)
