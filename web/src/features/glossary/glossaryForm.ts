// SPDX-License-Identifier: Apache-2.0
import type { Schemas } from '../../api/types'

export type Glossary = Schemas['Glossary']
export type GlossaryInput = Schemas['GlossaryInput']
export type GlossaryTerm = Schemas['GlossaryTerm']

/** One editable row: a term, its translations and whether to keep it as is. */
export interface TermRow {
  key: number
  term: string
  translations: Record<string, string>
  note: string
  /** Listed in the glossary's doNotTranslate. */
  keep: boolean
}

export interface GlossaryFormValues {
  name: string
  /** Translation columns, as language codes. */
  languages: string[]
  rows: TermRow[]
  /** doNotTranslate entries that aren't a row's term, one per line. */
  extraKeep: string
}

export const defaultLanguages = ['es', 'en']

let nextKey = 1
export function emptyRow(): TermRow {
  return { key: nextKey++, term: '', translations: {}, note: '', keep: false }
}

const fold = (s: string) => s.trim().toLocaleLowerCase()

export function valuesFrom(g?: Glossary): GlossaryFormValues {
  if (!g) return { name: '', languages: defaultLanguages, rows: [emptyRow()], extraKeep: '' }
  const keep = new Set(g.doNotTranslate.map(fold))
  const terms = new Set(g.terms.map((t) => fold(t.term)))
  const languages = [
    ...new Set([...defaultLanguages, ...g.terms.flatMap((t) => Object.keys(t.translations ?? {}))]),
  ]
  return {
    name: g.name,
    languages,
    rows: g.terms.map((t) => ({
      key: nextKey++,
      term: t.term,
      translations: { ...t.translations },
      note: t.note ?? '',
      keep: keep.has(fold(t.term)),
    })),
    extraKeep: g.doNotTranslate.filter((w) => !terms.has(fold(w))).join('\n'),
  }
}

/** The keys of the rows `toBody` sends, in order: `terms.N` in a 400's fields is the Nth. */
export function sentRowKeys(v: GlossaryFormValues): number[] {
  return v.rows.filter((r) => r.term.trim() !== '').map((r) => r.key)
}

/** The PUT/POST body; blank rows and blank cells are dropped. */
export function toBody(v: GlossaryFormValues): GlossaryInput {
  const rows = v.rows.filter((r) => r.term.trim() !== '')
  const terms: GlossaryTerm[] = rows.map((r) => {
    const translations = Object.fromEntries(
      Object.entries(r.translations)
        .filter(([lang, text]) => v.languages.includes(lang) && text.trim() !== '')
        .map(([lang, text]) => [lang, text.trim()]),
    )
    const term: GlossaryTerm = { term: r.term.trim() }
    if (Object.keys(translations).length > 0) term.translations = translations
    if (r.note.trim()) term.note = r.note.trim()
    return term
  })
  const keep = [
    ...rows.filter((r) => r.keep).map((r) => r.term.trim()),
    ...v.extraKeep.split(/[\n,]/).map((w) => w.trim()),
  ].filter(Boolean)
  const seen = new Set<string>()
  const doNotTranslate = keep.filter((w) => !seen.has(fold(w)) && seen.add(fold(w)))
  return { name: v.name.trim(), terms, doNotTranslate }
}

/** A language code as a column name: `es`, `en`, `pt-BR`. */
export const languagePattern = /^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$/

/** Splits pasted CSV or TSV (a spreadsheet selection) into cells. */
export function parseDelimited(text: string): string[][] {
  const first = text.split(/\r?\n/, 1)[0] ?? ''
  const delimiter = first.includes('\t')
    ? '\t'
    : (first.match(/;/g)?.length ?? 0) > (first.match(/,/g)?.length ?? 0)
      ? ';'
      : ','
  const rows: string[][] = []
  let row: string[] = []
  let cell = ''
  let quoted = false
  for (let i = 0; i < text.length; i++) {
    const c = text[i]
    if (quoted) {
      if (c === '"' && text[i + 1] === '"') {
        cell += '"'
        i++
      } else if (c === '"') quoted = false
      else cell += c
    } else if (c === '"' && cell === '') quoted = true
    else if (c === delimiter) {
      row.push(cell)
      cell = ''
    } else if (c === '\n' || c === '\r') {
      if (c === '\r' && text[i + 1] === '\n') i++
      row.push(cell)
      rows.push(row)
      row = []
      cell = ''
    } else cell += c
  }
  if (cell !== '' || row.length > 0) {
    row.push(cell)
    rows.push(row)
  }
  return rows.filter((r) => r.some((c) => c.trim() !== ''))
}

const truthy = new Set(['1', 'x', 'yes', 'y', 'true', 'si', 'sí', 's'])
const keepHeaders = new Set(['keep', 'dnt', 'donottranslate', 'do_not_translate', 'no_traducir'])
const noteHeaders = new Set(['note', 'nota', 'context', 'contexto'])
const termHeaders = new Set(['term', 'término', 'termino', 'source'])

export interface ImportResult {
  values: GlossaryFormValues
  added: number
  updated: number
}

/**
 * Merges pasted rows into the form. With a header row (first cell `term`),
 * columns are named: language codes, `note`, `keep`. Without one, the
 * columns are the term, then the form's language columns in order, then
 * the note. A term already in the table is updated in place.
 */
export function importRows(v: GlossaryFormValues, text: string): ImportResult {
  const table = parseDelimited(text)
  const head = table[0]?.map(fold)
  let columns: string[]
  let body = table
  if (head && termHeaders.has(head[0] ?? '')) {
    columns = head.map((h, i) =>
      i === 0
        ? 'term'
        : keepHeaders.has(h)
          ? 'keep'
          : noteHeaders.has(h)
            ? 'note'
            : languagePattern.test(table[0]![i]!.trim())
              ? table[0]![i]!.trim()
              : '',
    )
    body = table.slice(1)
  } else {
    columns = ['term', ...v.languages, 'note']
  }
  const languages = [...v.languages]
  for (const c of columns) {
    if (c && c !== 'term' && c !== 'note' && c !== 'keep' && !languages.includes(c))
      languages.push(c)
  }

  const rows = v.rows.filter((r) => r.term.trim() !== '' || r.note.trim() !== '')
  const index = new Map(rows.map((r, i) => [fold(r.term), i]))
  let added = 0
  let updated = 0
  for (const cells of body) {
    const term = cells[0]?.trim() ?? ''
    if (!term) continue
    const existing = index.get(fold(term))
    const row: TermRow =
      existing === undefined
        ? { ...emptyRow(), term }
        : { ...rows[existing]!, translations: { ...rows[existing]!.translations } }
    columns.forEach((col, i) => {
      const cell = cells[i]?.trim() ?? ''
      if (i === 0 || !col || cell === '') return
      if (col === 'note') row.note = cell
      else if (col === 'keep') row.keep = truthy.has(fold(cell))
      else row.translations[col] = cell
    })
    if (existing === undefined) {
      index.set(fold(term), rows.length)
      rows.push(row)
      added++
    } else {
      rows[existing] = row
      updated++
    }
  }
  return { values: { ...v, languages, rows }, added, updated }
}
