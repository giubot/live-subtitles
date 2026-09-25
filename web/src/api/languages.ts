// SPDX-License-Identifier: Apache-2.0
import { api } from './client'
import type { Schemas } from './types'

export type Language = Schemas['Language']
export type SourceLanguage = Schemas['SourceLanguage']

/** Offered while the catalog loads or when it can't be read, so forms never break. */
export const fallbackLanguages = ['es', 'en', 'pt', 'fr', 'de', 'it']
const fallbackSources = ['en', 'es']

export interface Languages {
  /** Every language captions can be translated into (codes). */
  targets: string[]
  /** `auto` plus the languages speech can be recognised in. */
  sources: SourceLanguage[]
  /** The catalog entry for a code, once loaded. */
  byCode: Map<string, Language>
}

/**
 * The server's language catalog (`GET /api/languages`). It hardly ever
 * changes, so it's fetched once per page load.
 */
export function useLanguages(): Languages {
  const { data } = api.useQuery('get', '/api/languages', {}, { staleTime: Infinity })
  const catalog = data ?? []
  const targets = catalog.length > 0 ? catalog.map((l) => l.code) : fallbackLanguages
  const sourceCodes =
    catalog.length > 0 ? catalog.filter((l) => l.canBeSource).map((l) => l.code) : fallbackSources
  return {
    targets,
    // The server decides which codes can be a source; the spec's enum follows it.
    sources: ['auto', ...(sourceCodes as SourceLanguage[])],
    byCode: new Map(catalog.map((l) => [l.code, l])),
  }
}
