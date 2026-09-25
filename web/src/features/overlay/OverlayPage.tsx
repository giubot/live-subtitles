// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { Caption } from '../../api/types'
import { emptyTrack, useCaptions } from '../../realtime/captions'
import type { WebSocketFactory } from '../../realtime/socket'
import { OverlayCaptions } from './OverlayCaptions'
import { isBuiltinPreset, lookFromStyle, resolveLook, type OverlaySearch } from './overlayStyle'

/** Captions kept in the DOM; the line window shows the end of them. */
const kept = 3

export interface OverlayPageProps {
  sessionId: string
  search: OverlaySearch
  WebSocket?: WebSocketFactory
}

/**
 * OBS/vMix browser source (OUT-5, UI-7): transparent, no MUI and no
 * chrome, styled only by the overlay preset and the link's overrides.
 * `?preset=` names a built-in or a saved preset (fetched without auth; if
 * it can't be loaded the classic look is used). Sized for 1920 × 1080 and
 * scaled with the source's height; the text fades out after `fadeAfter`
 * ms without a new caption.
 */
export function OverlayPage({ sessionId, search, WebSocket }: OverlayPageProps) {
  const savedId = search.preset && !isBuiltinPreset(search.preset) ? search.preset : undefined
  const saved = api.useQuery(
    'get',
    '/api/overlay-presets/{presetId}',
    { params: { path: { presetId: savedId ?? '' } } },
    { enabled: !!savedId, retry: 2, staleTime: Infinity },
  )
  const look = resolveLook(search, saved.data ? lookFromStyle(saved.data.style) : undefined)
  // Don't flash the default look while a saved preset is on its way.
  const styleReady = !savedId || !saved.isPending

  // The track: the link's lang, else the session's first language.
  const session = api.useQuery(
    'get',
    '/api/public/sessions/{sessionId}',
    { params: { path: { sessionId } } },
    { enabled: !search.lang },
  )
  const lang = search.lang ?? session.data?.languages[0]
  const captions = useCaptions(lang ? sessionId : undefined, lang ? [lang] : [], {
    history: 1,
    WebSocket,
  })
  const track = (lang && captions.tracks[lang]) || emptyTrack
  const shown: Caption[] = [
    ...track.finals.slice(-kept),
    ...(look.showInterim && track.interim ? [track.interim] : []),
  ].slice(-kept)

  // Fade after silence: remember which text the timer ran out on.
  const latest = shown.at(-1)
  const key = latest ? `${latest.segmentId}:${latest.text.length}:${latest.final}` : ''
  const [fadedKey, setFadedKey] = useState<string>()
  useEffect(() => {
    if (!key || look.fadeAfterMs <= 0) return
    const timer = setTimeout(() => setFadedKey(key), look.fadeAfterMs)
    return () => clearTimeout(timer)
  }, [key, look.fadeAfterMs])
  const visible = styleReady && shown.length > 0 && fadedKey !== key

  return (
    <OverlayCaptions
      look={look}
      visible={visible}
      lines={shown.map((c) => ({
        id: c.segmentId,
        text: c.text,
        lang: lang === 'source' ? c.sourceLang : c.lang,
      }))}
    />
  )
}
