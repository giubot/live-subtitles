// SPDX-License-Identifier: Apache-2.0
import { useQuery } from '@tanstack/react-query'
import { client } from '../../api/client'
import type { Caption } from '../../api/types'

/** Page size asked of the captions endpoint (its maximum). */
const pageSize = 1000
/** Safety stop: 50 pages is 50 000 cues, far more than a day of talks. */
const maxPages = 50

/**
 * Every final caption of one track of a recording, following the
 * endpoint's cursor until the last page. Hidden (moderated) cues are
 * dropped; the rest are sorted by start time.
 */
export function useTranscript(sessionId: string, recordingId?: string, lang?: string) {
  return useQuery({
    queryKey: ['replay-transcript', sessionId, recordingId, lang],
    enabled: !!recordingId && !!lang,
    staleTime: 60_000,
    queryFn: async ({ signal }) => {
      const cues: Caption[] = []
      let after: string | undefined
      for (let i = 0; i < maxPages; i++) {
        const { data, error } = await client.GET('/api/public/sessions/{sessionId}/captions', {
          params: {
            path: { sessionId },
            query: { lang: lang ?? '', recordingId, limit: pageSize, after },
          },
          signal,
        })
        if (error) throw error
        cues.push(...data.items)
        if (!data.nextCursor) break
        after = data.nextCursor
      }
      return cues.filter((c) => !c.hidden).sort((a, b) => a.start - b.start)
    },
  })
}
