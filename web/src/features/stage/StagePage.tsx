// SPDX-License-Identifier: Apache-2.0
import { useEffect, type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'
import { api, errorCode } from '../../api/client'
import type { Caption } from '../../api/types'
import { QrCode } from '../../components/QrCode'
import { emptyTrack, useCaptions } from '../../realtime/captions'
import type { WebSocketFactory } from '../../realtime/socket'
import { presetColours, stageOptions, type StageSearch } from './stageOptions'
import { useIdle } from './useIdle'

/** Cursor and hints hide after this long without movement. */
const idleMs = 3000
const sessionPollMs = 30000

export interface StagePageProps {
  sessionId: string
  search: StageSearch
  WebSocket?: WebSocketFactory
}

const label: CSSProperties = {
  fontFamily: 'var(--font-mono)',
  fontWeight: 500,
  fontSize: 'clamp(0.75rem, 1.4vw, 1.5rem)',
  lineHeight: 1.2,
  letterSpacing: 'var(--tracking-label)',
  textTransform: 'uppercase',
}

const visuallyHidden: CSSProperties = {
  position: 'absolute',
  inlineSize: 1,
  blockSize: 1,
  margin: -1,
  padding: 0,
  overflow: 'hidden',
  clipPath: 'inset(50%)',
  whiteSpace: 'nowrap',
  border: 0,
}

/** Last `n` lines of a track: finals, then the sentence in progress. */
function lastLines(finals: Caption[], interim: Caption | null, n: number): Caption[] {
  const all = interim ? [...finals, interim] : finals
  return all.slice(-n)
}

/**
 * Stage screen for a projector or TV (OUT-3, UI-7): the last few lines,
 * large and high-contrast in the session's preset, optionally a second
 * language underneath, and a QR code to the phone viewer. Captions appear
 * without animation; the cursor hides when the mouse rests.
 */
export function StagePage({ sessionId, search, WebSocket }: StagePageProps) {
  const { t } = useTranslation('stage')
  const session = api.useQuery(
    'get',
    '/api/public/sessions/{sessionId}',
    { params: { path: { sessionId } } },
    { refetchInterval: sessionPollMs },
  )
  const network = api.useQuery('get', '/api/network')
  const opts = stageOptions(search, session.data?.stageStyle, session.data?.languages ?? [])
  const tracks = [opts.lang, opts.lang2].filter((l): l is string => !!l)
  const captions = useCaptions(session.data ? sessionId : undefined, tracks, {
    history: opts.lines,
    WebSocket,
  })
  const idle = useIdle(idleMs)
  const colours = presetColours[opts.preset]
  const state = captions.state ?? session.data?.state

  const main = (opts.lang && captions.tracks[opts.lang]) || emptyTrack
  const second = (opts.lang2 && captions.tracks[opts.lang2]) || emptyTrack
  const lines = lastLines(main.finals, main.interim, opts.lines)
  const secondLine = lastLines(second.finals, second.interim, 1)[0]
  const langOf = (c: Caption, track?: string) => (track === 'source' ? c.sourceLang : c.lang)

  const viewerUrl = network.data
    ? `${network.data.viewerBaseUrl.replace(/\/$/, '')}/s/${encodeURIComponent(sessionId)}`
    : undefined

  const name = session.data?.name ?? sessionId
  useEffect(() => {
    document.title = t('title', { name })
  }, [name, t])

  // Double-click or F toggles full screen.
  useEffect(() => {
    const toggle = () => {
      if (document.fullscreenElement) void document.exitFullscreen?.()
      else void document.documentElement.requestFullscreen?.().catch(() => {})
    }
    const onKey = (e: KeyboardEvent) => {
      if ((e.key === 'f' || e.key === 'F') && !e.ctrlKey && !e.metaKey && !e.altKey) toggle()
    }
    window.addEventListener('dblclick', toggle)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('dblclick', toggle)
      window.removeEventListener('keydown', onKey)
    }
  }, [])

  const notFound = errorCode(session.error) === 'session.not_found'
  const status =
    captions.connection === 'reconnecting'
      ? t('reconnecting')
      : state === 'live'
        ? t('live', { name })
        : state === 'paused'
          ? t('paused', { name })
          : name

  return (
    <main
      data-preset={opts.preset}
      style={{
        position: 'relative',
        blockSize: '100dvh',
        overflow: 'hidden',
        display: 'grid',
        alignContent: 'end',
        gap: '1.2vw',
        padding: '5vw 5vw 6vw',
        backgroundColor: colours.bg,
        color: colours.fg,
        fontFamily: 'var(--font-body)',
        cursor: idle ? 'none' : 'default',
      }}
    >
      {/* The page heading for screen readers; the screen itself shows only captions. */}
      <h1 style={visuallyHidden}>{name}</h1>
      <div
        role="status"
        style={{
          ...label,
          position: 'absolute',
          insetBlockStart: '3.5vw',
          insetInlineStart: '5vw',
          display: 'flex',
          alignItems: 'center',
          gap: '0.8vw',
          color: colours.secondary,
        }}
      >
        {state === 'live' && captions.connection !== 'reconnecting' && (
          <span
            aria-hidden
            style={{
              inlineSize: '0.8em',
              blockSize: '0.8em',
              borderRadius: '50%',
              backgroundColor: 'var(--color-live)',
            }}
          />
        )}
        {status}
      </div>

      {opts.qr && viewerUrl && (
        <div
          style={{
            position: 'absolute',
            insetBlockStart: '3.5vw',
            insetInlineEnd: '3.5vw',
            display: 'grid',
            gap: '0.8vw',
            justifyItems: 'end',
          }}
        >
          <QrCode value={viewerUrl} size="11vw" />
          <span
            style={{
              ...label,
              textTransform: 'none',
              letterSpacing: 0,
              color: colours.secondary,
              textAlign: 'end',
            }}
          >
            {t('followQr')}
            <br />
            {viewerUrl.replace(/^https?:\/\//, '')}
          </span>
        </div>
      )}

      {notFound ? (
        <p style={{ margin: 0, fontSize: 'var(--text-2xl)' }}>{t('notFound', { id: sessionId })}</p>
      ) : lines.length === 0 ? (
        <p style={{ margin: 0, fontSize: 'var(--text-2xl)', color: colours.secondary }}>
          {t('waiting')}
        </p>
      ) : (
        <div aria-live="polite" style={{ display: 'grid', gap: '1.2vw' }}>
          {lines.map((c) => (
            <p
              key={c.segmentId}
              lang={langOf(c, opts.lang)}
              data-gap={c.gapBeforeMs ? true : undefined}
              style={{
                margin: 0,
                maxInlineSize: '36ch',
                // Audio was lost before this line (SES-5): a thin dashed
                // rule, no text, so it takes no line from the captions.
                ...(c.gapBeforeMs && {
                  borderBlockStart: `max(1px, 0.15vw) dashed ${colours.secondary}`,
                  paddingBlockStart: '0.6vw',
                }),
                fontWeight: 600,
                fontSize: 'var(--text-caption-stage)',
                lineHeight: 1.28,
              }}
            >
              {c.text}
            </p>
          ))}
        </div>
      )}

      {secondLine && (
        <p
          lang={langOf(secondLine, opts.lang2)}
          style={{
            margin: 0,
            maxInlineSize: '42ch',
            fontWeight: 500,
            fontSize: 'calc(var(--text-caption-stage) * 0.7)',
            lineHeight: 1.28,
            color: colours.secondary,
          }}
        >
          {secondLine.text}
        </p>
      )}

      <p
        aria-hidden={idle}
        style={{
          ...label,
          position: 'absolute',
          insetBlockEnd: '1.5vw',
          insetInlineEnd: '3.5vw',
          margin: 0,
          textTransform: 'none',
          letterSpacing: 0,
          color: colours.secondary,
          opacity: idle ? 0 : 1,
          transition: 'opacity var(--dur-short) var(--ease-out)',
        }}
      >
        {t('fullscreenHint')}
      </p>
    </main>
  )
}
