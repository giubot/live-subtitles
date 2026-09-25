// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { Caption } from '../../api/types'
import { emptyTrack, useCaptions } from '../../realtime/captions'
import type { WebSocketFactory } from '../../realtime/socket'
import { outlineShadow, resolveLook, vh, type OverlaySearch } from './overlayStyle'

/** Captions kept in the DOM; the line window shows the end of them. */
const kept = 3
const lineHeight = 1.3

export interface OverlayPageProps {
  sessionId: string
  search: OverlaySearch
  WebSocket?: WebSocketFactory
}

/**
 * OBS/vMix browser source (OUT-5, UI-7): transparent, no MUI and no
 * chrome, styled only by the overlay preset and the link's overrides.
 * Sized for 1920 × 1080 and scaled with the source's height. A window of
 * `maxLines` lines, bottom-aligned, so a long sentence shows its end; the
 * text fades out after `fadeAfter` ms without a new caption.
 */
export function OverlayPage({ sessionId, search, WebSocket }: OverlayPageProps) {
  const look = resolveLook(search)
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
  const visible = shown.length > 0 && fadedKey !== key

  const boxed = look.background !== 'transparent'
  const padBlock = boxed ? 0.15 : 0
  const padInline = boxed ? 0.4 : 0

  return (
    <div
      data-overlay
      style={{
        position: 'fixed',
        inset: 0,
        overflow: 'hidden',
        display: 'flex',
        flexDirection: 'column',
        justifyContent: look.position === 'top' ? 'flex-start' : 'flex-end',
        alignItems: { left: 'flex-start', center: 'center', right: 'flex-end' }[look.align],
        padding: `${vh(look.marginPx)} ${vh(look.marginPx * 1.6)}`,
        pointerEvents: 'none',
        background: 'transparent',
      }}
    >
      <div
        aria-live="polite"
        style={{
          maxInlineSize: '42ch',
          maxBlockSize: `calc(${look.maxLines * lineHeight}em + ${padBlock * 2}em)`,
          overflow: 'hidden',
          display: 'flex',
          flexDirection: 'column',
          justifyContent: 'flex-end',
          fontFamily: 'var(--font-body)',
          fontSize: vh(look.fontSizePx),
          fontWeight: look.fontWeight,
          lineHeight,
          textAlign: look.align,
          color: look.color,
          textShadow: outlineShadow(look.outlineWidthPx, look.outlineColor),
          opacity: visible ? 1 : 0,
          transition: 'opacity var(--dur-long) var(--ease-out)',
        }}
      >
        {shown.map((c) => (
          <p
            key={c.segmentId}
            lang={lang === 'source' ? c.sourceLang : c.lang}
            style={{ margin: 0 }}
          >
            <span
              style={{
                backgroundColor: look.background,
                padding: boxed ? `${padBlock}em ${padInline}em` : undefined,
                borderRadius: boxed ? 'var(--radius-chip)' : undefined,
                boxDecorationBreak: 'clone',
                WebkitBoxDecorationBreak: 'clone',
              }}
            >
              {c.text}
            </span>
          </p>
        ))}
      </div>
    </div>
  )
}
