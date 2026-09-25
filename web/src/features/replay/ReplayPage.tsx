// SPDX-License-Identifier: Apache-2.0
import DownloadOutlined from '@mui/icons-material/DownloadOutlined'
import MicOffOutlined from '@mui/icons-material/MicOffOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { Link } from '@tanstack/react-router'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import type { Caption } from '../../api/types'
import { EmptyState } from '../../components/EmptyState'
import { ErrorAlert } from '../../components/ErrorAlert'
import { LanguagePicker } from '../../components/LanguagePicker'
import { Notice } from '../../components/Notice'
import { ThemeToggle } from '../../components/ThemeToggle'
import { UiLanguageSwitcher } from '../../components/UiLanguageSwitcher'
import { nativeLanguageName, sourceTrack } from '../../components/languageNames'
import { currentLanguage } from '../../i18n'
import { timecode } from '../viewer/format'
import { langStorageKey, pickTrack, readPref, writePref } from '../viewer/prefs'
import {
  audioUrl,
  cueAt,
  newestFirst,
  pickRecording,
  subtitlesUrl,
  type DownloadFormat,
  type Recording,
} from './replay'
import { Transcript } from './Transcript'
import { useTranscript } from './useTranscript'

const formats: DownloadFormat[] = ['srt', 'vtt', 'txt']

export interface ReplayPageProps {
  sessionId: string
  /** Recording to open first (`?recording=`); defaults to the newest. */
  recordingId?: string
}

/**
 * Replay (REC-4, OUT-8): a recording's audio with a clickable transcript
 * that follows playback, one `<track>` per language, and downloads.
 */
export function ReplayPage({ sessionId, recordingId }: ReplayPageProps) {
  const { t, i18n } = useTranslation('replay')
  const session = api.useQuery('get', '/api/public/sessions/{sessionId}', {
    params: { path: { sessionId } },
  })
  const recordingsQuery = api.useQuery('get', '/api/recordings', {
    params: { query: { sessionId } },
  })
  const recordings = useMemo(() => newestFirst(recordingsQuery.data ?? []), [recordingsQuery.data])
  const [chosenRecording, setChosenRecording] = useState<string | undefined>(recordingId)
  const recording = pickRecording(recordings, chosenRecording)

  const languages = recording?.languages.length
    ? recording.languages
    : (session.data?.languages ?? [])
  const [chosenLang, setChosenLang] = useState<string>()
  const track = pickTrack(languages, {
    requested: chosenLang,
    stored: readPref(langStorageKey),
    uiLanguage: currentLanguage(),
  })

  const transcript = useTranscript(sessionId, recording?.id, track)
  const cues = transcript.data ?? []
  const audioRef = useRef<HTMLAudioElement>(null)
  const [time, setTime] = useState(0)
  const current = cueAt(cues, time)

  const name = session.data?.name ?? sessionId
  const title = t('title', { id: name })
  useEffect(() => {
    document.title = title
  }, [title])

  const dateFormat = useMemo(
    () =>
      new Intl.DateTimeFormat(i18n.resolvedLanguage, { dateStyle: 'medium', timeStyle: 'short' }),
    [i18n.resolvedLanguage],
  )
  const recordingLabel = (r: Recording) => {
    const date = r.title ?? dateFormat.format(new Date(r.startedAt))
    if (r.status === 'recording') return t('recording.inProgress', { date })
    if (r.status === 'failed') return t('recording.failed', { date })
    return r.durationSec != null
      ? t('recording.option', { date, duration: timecode(r.durationSec) })
      : date
  }

  const changeLang = (l: string) => {
    setChosenLang(l)
    writePref(langStorageKey, l)
  }
  const seek = (seconds: number) => {
    const audio = audioRef.current
    if (!audio) return
    audio.currentTime = seconds
    setTime(seconds)
    void audio.play().catch(() => undefined)
  }
  const langOf = (c: Caption) => (track === sourceTrack ? c.sourceLang : c.lang)
  const tracks = [...languages, sourceTrack]
  const error: unknown = session.error ?? recordingsQuery.error

  return (
    <Box
      component="main"
      sx={{
        minBlockSize: '100dvh',
        display: 'grid',
        gridTemplateRows: 'auto auto minmax(0, 1fr) auto',
        maxInlineSize: '48rem',
        marginInline: 'auto',
        backgroundColor: 'var(--color-paper)',
        '@media (min-width: 49rem)': {
          borderInline: 'var(--rule-hair) solid var(--color-rule)',
        },
      }}
    >
      <Box
        component="header"
        sx={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          gap: 'var(--space-xs) var(--space-sm)',
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
          {title}
        </Typography>
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-xs)' }}>
          <UiLanguageSwitcher />
          <ThemeToggle />
        </Box>
      </Box>

      <Box
        sx={{
          display: 'grid',
          gap: 'var(--space-sm)',
          paddingBlock: 'var(--space-sm) var(--space-2xs)',
          paddingInline: 'var(--space-md)',
          borderBlockEnd: 'var(--rule-hair) solid var(--color-rule)',
          backgroundColor: 'var(--color-paper-2)',
        }}
      >
        {error != null && (
          <ErrorAlert
            error={error}
            action={
              <Button component={Link} to="/s" variant="outlined" color="secondary">
                {t('allSessions')}
              </Button>
            }
          />
        )}
        {recording && (
          <>
            <Box
              sx={{
                display: 'flex',
                flexWrap: 'wrap',
                alignItems: 'flex-end',
                gap: 'var(--space-xs) var(--space-sm)',
              }}
            >
              {recordings.length > 1 && (
                <TextField
                  select
                  label={t('recording.label')}
                  value={recording.id}
                  onChange={(e) => {
                    setChosenRecording(e.target.value)
                    setTime(0)
                  }}
                  helperText=" "
                  sx={{ flex: '1 1 14rem', minInlineSize: 0 }}
                  slotProps={{ select: { native: true } }}
                >
                  {recordings.map((r) => (
                    <option key={r.id} value={r.id}>
                      {recordingLabel(r)}
                    </option>
                  ))}
                </TextField>
              )}
              <LanguagePicker
                value={track ?? ''}
                onChange={changeLang}
                languages={languages}
                includeSource
                disabled={!track}
                sx={{ flex: '1 1 10rem', minInlineSize: 0 }}
              />
            </Box>
            {recording.status === 'recording' && <Notice>{t('stillRecording')}</Notice>}
            <Box
              component="audio"
              key={recording.id}
              ref={audioRef}
              controls
              preload="metadata"
              src={audioUrl(recording.id)}
              aria-label={t('player')}
              onTimeUpdate={(e) => setTime(e.currentTarget.currentTime)}
              onSeeked={(e) => setTime(e.currentTarget.currentTime)}
              sx={{ inlineSize: '100%', marginBlockEnd: 'var(--space-sm)' }}
            >
              {tracks.map((l) => (
                <track
                  key={l}
                  kind="subtitles"
                  srcLang={l === sourceTrack ? undefined : l}
                  label={l === sourceTrack ? t('sourceTrack') : nativeLanguageName(l)}
                  src={subtitlesUrl(sessionId, recording.id, l, 'vtt')}
                  default={l === track}
                />
              ))}
            </Box>
          </>
        )}
      </Box>

      <Box
        sx={{
          overflowY: 'auto',
          padding: 'var(--space-md)',
          display: 'grid',
          alignContent: 'start',
          gap: 'var(--space-md)',
        }}
      >
        {recordingsQuery.isSuccess && recordings.length === 0 && (
          <EmptyState
            icon={<MicOffOutlined />}
            title={t('noRecordings.title')}
            description={t('noRecordings.body')}
            action={
              <Button component={Link} to="/s" variant="outlined" color="secondary">
                {t('allSessions')}
              </Button>
            }
          />
        )}
        {transcript.error && <ErrorAlert error={transcript.error} />}
        {transcript.isPending && recording && track && (
          <Typography sx={{ color: 'var(--color-muted)' }}>{t('loading')}</Typography>
        )}
        {transcript.isSuccess && cues.length === 0 && (
          <Typography sx={{ color: 'var(--color-muted)' }}>{t('transcriptEmpty')}</Typography>
        )}
        {cues.length > 0 && (
          <Transcript cues={cues} current={current} langOf={langOf} onSeek={seek} />
        )}
      </Box>

      {recording && track && (
        <Box
          component="footer"
          sx={{
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            gap: 'var(--space-xs)',
            paddingBlock: 'var(--space-sm)',
            paddingInline: 'var(--space-md)',
            borderBlockStart: 'var(--rule-hair) solid var(--color-rule)',
          }}
        >
          <Typography variant="overline" component="span" sx={{ marginInlineEnd: 'auto' }}>
            {t('downloads.label')}
          </Typography>
          <Button
            component="a"
            href={audioUrl(recording.id)}
            download={`${sessionId}.m4a`}
            variant="outlined"
            color="secondary"
            size="small"
            startIcon={<DownloadOutlined aria-hidden />}
          >
            {t('downloads.audio')}
          </Button>
          {formats.map((f) => (
            <Button
              key={f}
              component="a"
              href={subtitlesUrl(sessionId, recording.id, track, f)}
              download
              variant="outlined"
              color="secondary"
              size="small"
              startIcon={<DownloadOutlined aria-hidden />}
            >
              {t(`downloads.${f}`)}
            </Button>
          ))}
        </Box>
      )}
    </Box>
  )
}
