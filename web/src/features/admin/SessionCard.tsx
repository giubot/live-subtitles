// SPDX-License-Identifier: Apache-2.0
import EditOutlined from '@mui/icons-material/EditOutlined'
import ExpandLessOutlined from '@mui/icons-material/ExpandLessOutlined'
import LinkOutlined from '@mui/icons-material/LinkOutlined'
import PauseOutlined from '@mui/icons-material/PauseOutlined'
import PlayArrowOutlined from '@mui/icons-material/PlayArrowOutlined'
import StopOutlined from '@mui/icons-material/StopOutlined'
import VolumeOffOutlined from '@mui/icons-material/VolumeOffOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Typography from '@mui/material/Typography'
import { useQueryClient } from '@tanstack/react-query'
import { useId, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import type { SessionStatus } from '../../api/types'
import { ErrorAlert } from '../../components/ErrorAlert'
import { LevelMeter } from '../../components/LevelMeter'
import { Notice } from '../../components/Notice'
import { Panel } from '../../components/Panel'
import { Stat } from '../../components/Stat'
import { StatusChip } from '../../components/StatusChip'
import { useAdminEventsStore } from '../../realtime/admin'
import { stateChip } from '../viewer/format'
import { SessionLinks } from './SessionLinks'
import { ccChip, type Session } from './sessionForm'

export interface SessionCardProps {
  session: Session
  /** Live status from /ws/admin, when known. */
  status?: SessionStatus
  /** The ingest token, if this page created or rotated it. */
  token?: string
  onToken: (token: string) => void
  expanded: boolean
  onToggleLinks: () => void
  onEdit: () => void
}

/**
 * One session on the live dashboard (ADM-1, SES-4, AUD-6): state, controls,
 * input level and flags, provider, detected language, latency per track,
 * viewers, SRT and stream-caption readouts, recent errors, and its links.
 * Readouts the server hasn't sent yet are left out.
 */
export function SessionCard({
  session,
  status,
  token,
  onToken,
  expanded,
  onToggleLinks,
  onEdit,
}: SessionCardProps) {
  const { t, i18n } = useTranslation('admin')
  const linksId = useId()
  const queryClient = useQueryClient()
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['get', '/api/sessions'] })
  const path = { params: { path: { sessionId: session.id } } }
  const start = api.useMutation('post', '/api/sessions/{sessionId}/start', { onSettled: refresh })
  const pause = api.useMutation('post', '/api/sessions/{sessionId}/pause', { onSettled: refresh })
  const stop = api.useMutation('post', '/api/sessions/{sessionId}/stop', { onSettled: refresh })
  const actionError: unknown = start.error ?? pause.error ?? stop.error
  const busy = start.isPending || pause.isPending || stop.isPending

  const state = status?.state ?? session.state
  const audio = status?.audio
  const srt = status?.srt
  const cc = status?.streamCaptions
  const logs = useAdminEventsStore((st) => st.logs)
  const recent = useMemo(
    () =>
      logs
        .filter(
          (ev) =>
            ev.log?.sessionId === session.id &&
            ev.log.level === 'error' &&
            ev.log.code !== status?.error?.code,
        )
        .slice(-3),
    [logs, session.id, status?.error?.code],
  )
  const num = (value: number, digits = 1) =>
    new Intl.NumberFormat(i18n.language, { maximumFractionDigits: digits }).format(value)
  const clock = (at: string) =>
    new Date(at).toLocaleTimeString(i18n.language, {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  const latency = Object.entries(status?.latency ?? {}).map(
    ([track, l]) =>
      `${track === 'source' ? t('card.sourceTrack') : track.toUpperCase()} ${t('card.seconds', {
        value: new Intl.NumberFormat(i18n.language, {
          minimumFractionDigits: 1,
          maximumFractionDigits: 1,
        }).format(l.p95Ms / 1000),
      })}`,
  )
  const room = [
    session.room,
    (session.targetLanguages ?? []).map((l) => l.toUpperCase()).join(' · '),
  ]
    .filter(Boolean)
    .join(' — ')

  return (
    <Panel tone={state === 'error' ? 'error' : 'default'} sx={{ gap: 'var(--space-md)' }}>
      <Box
        component="article"
        aria-label={session.name}
        sx={{ display: 'grid', gap: 'var(--space-md)' }}
      >
        <Box
          sx={{
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            gap: 'var(--space-xs) var(--space-sm)',
          }}
        >
          <Typography variant="h2" sx={{ fontSize: 'var(--text-md)' }}>
            {session.name}
          </Typography>
          <StatusChip status={stateChip[state]} label={t(`state.${state}`)} />
          <Typography variant="body2" sx={{ color: 'var(--color-muted)', marginInlineEnd: 'auto' }}>
            {room}
          </Typography>
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-xs)' }}>
            {(state === 'idle' || state === 'error') && (
              <Button
                variant="outlined"
                color="secondary"
                size="small"
                startIcon={<PlayArrowOutlined aria-hidden />}
                loading={start.isPending}
                disabled={busy}
                onClick={() => start.mutate(path)}
              >
                {t('card.start')}
              </Button>
            )}
            {state === 'paused' && (
              <Button
                variant="outlined"
                color="secondary"
                size="small"
                startIcon={<PlayArrowOutlined aria-hidden />}
                loading={start.isPending}
                disabled={busy}
                onClick={() => start.mutate(path)}
              >
                {t('card.resume')}
              </Button>
            )}
            {state === 'live' && (
              <Button
                variant="outlined"
                color="secondary"
                size="small"
                startIcon={<PauseOutlined aria-hidden />}
                loading={pause.isPending}
                disabled={busy}
                onClick={() => pause.mutate(path)}
              >
                {t('card.pause')}
              </Button>
            )}
            {(state === 'live' || state === 'paused' || state === 'starting') && (
              <Button
                variant="outlined"
                color="error"
                size="small"
                startIcon={<StopOutlined aria-hidden />}
                loading={stop.isPending}
                disabled={busy}
                onClick={() => stop.mutate(path)}
              >
                {t('card.stop')}
              </Button>
            )}
            <Button
              variant="text"
              color="secondary"
              size="small"
              startIcon={<EditOutlined aria-hidden />}
              onClick={onEdit}
            >
              {t('card.edit')}
            </Button>
            <Button
              variant="text"
              color="secondary"
              size="small"
              startIcon={
                expanded ? <ExpandLessOutlined aria-hidden /> : <LinkOutlined aria-hidden />
              }
              aria-expanded={expanded}
              aria-controls={linksId}
              onClick={onToggleLinks}
            >
              {t('card.links')}
            </Button>
          </Box>
        </Box>

        <Box
          sx={{
            display: 'grid',
            gap: 'var(--space-md)',
            gridTemplateColumns: 'repeat(auto-fill, minmax(7rem, 1fr))',
          }}
        >
          <Stat
            label={t('card.input', { source: t(`source.${audio?.source ?? 'browser'}`) })}
            value={
              <LevelMeter db={audio?.connected ? (audio.levelDbfs ?? -Infinity) : -Infinity} />
            }
            detail={audio?.connected ? undefined : t('card.noCapture')}
            sx={{ gridColumn: '1 / -1', '@media (min-width: 30rem)': { gridColumn: 'span 2' } }}
          />
          <Stat
            label={t('card.provider')}
            value={t(`provider.${status?.provider ?? session.effectiveProvider}`)}
          />
          <Stat label={t('card.speaking')} value={status?.detectedLanguage?.toUpperCase() ?? '—'} />
          {latency.length > 0 && (
            <Stat
              label={t('card.latency')}
              value={latency[0]}
              detail={latency.length > 1 ? latency.slice(1).join(' · ') : undefined}
            />
          )}
          <Stat label={t('card.viewers')} value={String(status?.viewers ?? 0)} />
          {srt && (
            <Stat
              label={t('card.srt')}
              value={
                srt.connected === false
                  ? t('card.srtDown')
                  : srt.rttMs != null
                    ? t('card.srtRtt', { value: num(srt.rttMs, 0) })
                    : t('card.srtUp')
              }
              detail={
                [
                  srt.packetLossPct != null && t('card.srtLoss', { value: num(srt.packetLossPct) }),
                  srt.bitrateKbps != null &&
                    t('card.srtBitrate', { value: num(srt.bitrateKbps, 0) }),
                ]
                  .filter(Boolean)
                  .join(' · ') || undefined
              }
            />
          )}
          {cc && cc.state !== 'disabled' && (
            <Stat
              label={t('card.streamCaptions')}
              value={<StatusChip status={ccChip[cc.state]} label={t(`cc.state.${cc.state}`)} />}
              detail={
                [
                  cc.lastSeq != null && t('card.ccSeq', { seq: cc.lastSeq }),
                  cc.lastSentAt && t('card.ccSent', { time: clock(cc.lastSentAt) }),
                ]
                  .filter(Boolean)
                  .join(' · ') || undefined
              }
            />
          )}
        </Box>

        {audio?.connected && audio.clipping && <Notice>{t('card.clipping')}</Notice>}
        {audio?.connected && audio.silent && state === 'live' && (
          <Notice icon={<VolumeOffOutlined aria-hidden />}>{t('card.silent')}</Notice>
        )}
        {status?.error && state === 'error' && <ErrorAlert error={status.error} />}
        {cc?.error && cc.state === 'error' && <ErrorAlert error={cc.error} />}
        {recent.map((ev, i) => (
          <ErrorAlert key={`${ev.at}-${i}`} error={ev.log} />
        ))}
        {actionError != null && <ErrorAlert error={actionError} />}

        {expanded && (
          <Box id={linksId}>
            <SessionLinks session={session} token={token} onToken={onToken} />
          </Box>
        )}
      </Box>
    </Panel>
  )
}
