// SPDX-License-Identifier: Apache-2.0
import KeyboardArrowDownOutlined from '@mui/icons-material/KeyboardArrowDownOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Container from '@mui/material/Container'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Typography from '@mui/material/Typography'
import { Link } from '@tanstack/react-router'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, errorCode } from '../../api/client'
import type { Caption } from '../../api/types'
import { ErrorAlert } from '../../components/ErrorAlert'
import { LanguagePicker } from '../../components/LanguagePicker'
import { Notice } from '../../components/Notice'
import { StatusChip } from '../../components/StatusChip'
import { ThemeToggle } from '../../components/ThemeToggle'
import { UiLanguageSwitcher } from '../../components/UiLanguageSwitcher'
import { segmentedSx } from '../../components/segmented'
import { currentLanguage } from '../../i18n'
import { emptyTrack, useCaptions } from '../../realtime/captions'
import type { WebSocketFactory } from '../../realtime/socket'
import { gapSeconds, stateChip, timecode } from './format'
import {
  langStorageKey,
  pickTrack,
  readPref,
  readSizeStep,
  sizeStep,
  sizeSteps,
  sizeStorageKey,
  writePref,
} from './prefs'
import { useStickToBottom } from './useStickToBottom'

/** How often the session's details (name, languages) are refreshed. */
const sessionPollMs = 15000

export interface ViewerPageProps {
  sessionId: string
  /** Track from the link (`?lang=`), if any. */
  lang?: string
  WebSocket?: WebSocketFactory
}

/**
 * Audience viewer (OUT-2, OUT-11): caption-first, newest line at the
 * bottom, the sentence in progress in muted ink with a cobalt caret.
 * Finals go to a polite live region; the interim line is hidden from
 * screen readers so they aren't read word by word.
 */
export function ViewerPage({ sessionId, lang: linkLang, WebSocket }: ViewerPageProps) {
  const { t } = useTranslation('viewer')
  const session = api.useQuery(
    'get',
    '/api/public/sessions/{sessionId}',
    { params: { path: { sessionId } } },
    { refetchInterval: sessionPollMs },
  )
  const [chosen, setChosen] = useState<string>()
  const languages = session.data?.languages ?? []
  const track = pickTrack(languages, {
    requested: chosen ?? linkLang,
    stored: readPref(langStorageKey),
    uiLanguage: currentLanguage(),
  })
  const captions = useCaptions(
    session.data && track ? sessionId : undefined,
    track ? [track] : [],
    {
      WebSocket,
    },
  )
  const current = (track && captions.tracks[track]) || emptyTrack
  const state = captions.state ?? session.data?.state
  const [size, setSize] = useState(readSizeStep)

  const logRef = useRef<HTMLDivElement>(null)
  const { atBottom, onScroll, jumpToLive } = useStickToBottom(logRef, current)

  const name = session.data?.name ?? sessionId
  const title = t('title', { id: name })
  useEffect(() => {
    document.title = title
  }, [title])

  if (errorCode(session.error) === 'session.not_found') {
    return (
      <Container component="main" maxWidth="sm" sx={{ paddingBlock: 'var(--space-lg)' }}>
        <Typography variant="h2" component="h1" sx={{ marginBlockEnd: 'var(--space-md)' }}>
          {sessionId}
        </Typography>
        <ErrorAlert
          error={session.error}
          action={
            <Button component={Link} to="/s" variant="outlined" color="secondary">
              {t('allSessions')}
            </Button>
          }
        />
      </Container>
    )
  }

  const changeTrack = (l: string) => {
    setChosen(l)
    writePref(langStorageKey, l)
  }
  const changeSize = (step: number) => {
    setSize(step)
    writePref(sizeStorageKey, String(step))
  }
  const lineLang = (c: Caption) => (track === 'source' ? c.sourceLang : c.lang)
  const lastFinal = current.finals.length - 1
  const empty = current.finals.length === 0 && !current.interim

  return (
    <Box
      component="main"
      sx={{
        blockSize: '100dvh',
        display: 'grid',
        gridTemplateRows: 'auto auto minmax(0, 1fr) auto',
        maxInlineSize: '44rem',
        marginInline: 'auto',
        backgroundColor: 'var(--color-paper)',
        '@media (min-width: 45rem)': {
          borderInline: 'var(--rule-hair) solid var(--color-rule)',
        },
      }}
    >
      <Box
        component="header"
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--space-xs)',
          paddingBlock: 'var(--space-sm)',
          paddingInline: 'var(--space-md)',
          borderBlockEnd: 'var(--rule-hair) solid var(--color-rule)',
        }}
      >
        <Typography
          variant="h5"
          component="h1"
          sx={{ marginInlineEnd: 'auto', minInlineSize: 0, overflowWrap: 'anywhere' }}
        >
          {name}
        </Typography>
        {state && <StatusChip status={stateChip[state]} label={t(`state.${state}`)} />}
      </Box>

      <Box
        sx={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'flex-end',
          gap: 'var(--space-xs) var(--space-sm)',
          paddingBlock: 'var(--space-sm) var(--space-2xs)',
          paddingInline: 'var(--space-md)',
          borderBlockEnd: 'var(--rule-hair) solid var(--color-rule)',
          backgroundColor: 'var(--color-paper-2)',
        }}
      >
        <LanguagePicker
          value={track ?? ''}
          onChange={changeTrack}
          languages={languages}
          includeSource
          disabled={!track}
          sx={{ flex: '1 1 10rem', minInlineSize: 0 }}
        />
        <Box
          sx={{
            display: 'flex',
            flexWrap: 'wrap',
            gap: 'var(--space-xs)',
            paddingBlockEnd: 'var(--space-md)',
          }}
        >
          <ToggleButtonGroup
            aria-label={t('size.label')}
            value={null}
            exclusive
            onChange={(_, v: 'smaller' | 'larger' | null) => {
              if (v === 'smaller') changeSize(size - 1)
              if (v === 'larger') changeSize(size + 1)
            }}
            sx={segmentedSx}
          >
            <ToggleButton value="smaller" aria-label={t('size.smaller')} disabled={size === 0}>
              A−
            </ToggleButton>
            <ToggleButton
              value="larger"
              aria-label={t('size.larger')}
              disabled={size === sizeSteps.length - 1}
            >
              A+
            </ToggleButton>
          </ToggleButtonGroup>
          <UiLanguageSwitcher />
          <ThemeToggle />
        </Box>
      </Box>

      <Box
        ref={logRef}
        onScroll={onScroll}
        tabIndex={0}
        aria-label={t('transcript')}
        sx={{
          overflowY: 'auto',
          overscrollBehavior: 'contain',
          padding: 'var(--space-md)',
          display: 'grid',
          alignContent: 'end',
          gap: 'var(--space-md)',
          '--caption-size': `calc(var(--text-caption) * ${sizeStep(size)})`,
        }}
      >
        {session.error && <ErrorAlert error={session.error} />}
        {empty && (
          <Typography sx={{ color: 'var(--color-muted)' }}>
            {t(state === 'live' ? 'empty.live' : 'empty.waiting')}
          </Typography>
        )}
        <Box role="log" aria-live="polite" sx={{ display: 'grid', gap: 'var(--space-md)' }}>
          {current.finals.map((c, i) => (
            <Line
              key={c.segmentId}
              caption={c}
              lang={lineLang(c)}
              tone={i < lastFinal ? 'old' : 'final'}
            />
          ))}
        </Box>
        {current.interim && (
          <Line caption={current.interim} lang={lineLang(current.interim)} tone="interim" />
        )}
      </Box>

      <Box
        component="footer"
        sx={{
          display: 'grid',
          gap: 'var(--space-xs)',
          paddingBlock: 'var(--space-xs) var(--space-sm)',
          paddingInline: 'var(--space-md)',
          borderBlockStart: 'var(--rule-hair) solid var(--color-rule)',
        }}
      >
        {captions.connection === 'reconnecting' && <Notice>{t('reconnecting')}</Notice>}
        <Box
          sx={{
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 'var(--space-xs)',
            minBlockSize: '2rem',
          }}
        >
          {session.data?.recording ? (
            <Box
              component="span"
              sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--space-2xs)',
                fontSize: 'var(--text-xs)',
                color: 'var(--color-muted)',
                '&::before': {
                  content: '""',
                  inlineSize: '0.45rem',
                  blockSize: '0.45rem',
                  borderRadius: '50%',
                  backgroundColor: 'var(--color-live)',
                },
              }}
            >
              {t('recorded')}
            </Box>
          ) : (
            <span />
          )}
          {!atBottom && (
            <Button
              variant="outlined"
              color="secondary"
              size="small"
              startIcon={<KeyboardArrowDownOutlined aria-hidden />}
              onClick={jumpToLive}
            >
              {t('jumpToLive')}
            </Button>
          )}
        </Box>
      </Box>
    </Box>
  )
}

function Line({
  caption,
  lang,
  tone,
}: {
  caption: Caption
  lang: string
  tone: 'old' | 'final' | 'interim'
}) {
  return (
    <Box
      aria-hidden={tone === 'interim' || undefined}
      data-tone={tone}
      sx={{ display: 'grid', gap: 'var(--space-3xs)' }}
    >
      {!!caption.gapBeforeMs && <GapMarker ms={caption.gapBeforeMs} />}
      <Box
        component="time"
        sx={{
          fontFamily: 'var(--font-mono)',
          fontSize: 'var(--text-xs)',
          lineHeight: 1,
          color: 'var(--color-muted)',
          fontVariantNumeric: 'tabular-nums',
        }}
      >
        {timecode(caption.start)}
      </Box>
      <Box
        component="p"
        lang={lang}
        sx={{
          margin: 0,
          fontFamily: 'var(--font-body)',
          fontWeight: 'var(--weight-caption)',
          fontSize: 'var(--caption-size)',
          lineHeight: 'var(--leading-caption)',
          maxInlineSize: 'var(--measure)',
          color:
            tone === 'interim'
              ? 'var(--color-muted)'
              : tone === 'old'
                ? 'var(--color-neutral)'
                : 'var(--color-ink)',
          ...(tone === 'interim' && {
            '&::after': {
              content: '""',
              display: 'inline-block',
              inlineSize: '0.5ch',
              blockSize: '1em',
              marginInlineStart: 'var(--space-3xs)',
              verticalAlign: '-0.15em',
              backgroundColor: 'var(--color-accent)',
            },
          }),
        }}
      >
        {caption.text}
      </Box>
    </Box>
  )
}

/**
 * A quiet rule before a caption that follows lost audio (SES-5): the
 * provider or the audio source restarted, or the capture station reconnected.
 */
function GapMarker({ ms }: { ms: number }) {
  const { t } = useTranslation('viewer')
  return (
    <Box
      data-gap
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--space-xs)',
        marginBlockEnd: 'var(--space-2xs)',
        fontSize: 'var(--text-xs)',
        lineHeight: 1,
        color: 'var(--color-muted)',
        '&::before, &::after': {
          content: '""',
          flex: '1 1 0',
          borderBlockStart: 'var(--rule-hair) dashed var(--color-rule-2)',
        },
      }}
    >
      {t('gap', { seconds: gapSeconds(ms) })}
    </Box>
  )
}
