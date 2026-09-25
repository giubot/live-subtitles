// SPDX-License-Identifier: Apache-2.0
import DeleteOutlined from '@mui/icons-material/DeleteOutlined'
import DownloadOutlined from '@mui/icons-material/DownloadOutlined'
import GraphicEqOutlined from '@mui/icons-material/GraphicEqOutlined'
import PlayArrowOutlined from '@mui/icons-material/PlayArrowOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useQueryClient } from '@tanstack/react-query'
import { createLink } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { EmptyState } from '../../components/EmptyState'
import { ErrorAlert } from '../../components/ErrorAlert'
import { Panel } from '../../components/Panel'
import { Stat } from '../../components/Stat'
import { StatusChip } from '../../components/StatusChip'
import { AdminPage } from '../admin/AdminLayout'
import { timecode } from '../viewer/format'
import { audioUrl, newestFirst, type Recording } from '../replay/replay'
import { durationOf, formatBytes, recordingChip } from './format'

/** A MUI button that navigates like a router `Link`, route params type-checked. */
const ButtonLink = createLink(Button)

/** `/admin/recordings` (REC-5): recorded sessions, their disk use, replay and delete. */
export function RecordingsPage() {
  const { t, i18n } = useTranslation('recordings')
  const locale = i18n.resolvedLanguage
  const [sessionId, setSessionId] = useState('')
  const sessions = api.useQuery('get', '/api/sessions')
  const usage = api.useQuery('get', '/api/recordings/usage')
  const recordings = api.useQuery('get', '/api/recordings', {
    params: { query: sessionId ? { sessionId } : {} },
  })
  const listError: unknown = recordings.error
  const list = useMemo(() => newestFirst(recordings.data ?? []), [recordings.data])
  const sessionNames = useMemo(
    () => new Map((sessions.data ?? []).map((s) => [s.id, s.name])),
    [sessions.data],
  )

  return (
    <AdminPage title={t('title')}>
      <Box sx={{ display: 'grid', gap: 'var(--space-md)', maxInlineSize: '64rem' }}>
        <Panel title={t('usage.title')}>
          {usage.error ? (
            <ErrorAlert error={usage.error} />
          ) : (
            <Box
              sx={{
                display: 'grid',
                gap: 'var(--space-md)',
                gridTemplateColumns: 'repeat(auto-fit, minmax(10rem, 1fr))',
              }}
            >
              <Stat
                label={t('usage.used')}
                value={usage.data ? formatBytes(usage.data.usedBytes, locale) : '–'}
              />
              <Stat
                label={t('usage.free')}
                value={
                  usage.data?.freeBytes != null ? formatBytes(usage.data.freeBytes, locale) : '–'
                }
              />
              <Stat
                label={t('usage.count')}
                value={
                  usage.data ? new Intl.NumberFormat(locale).format(usage.data.recordings) : '–'
                }
              />
            </Box>
          )}
        </Panel>

        <Panel
          title={t('list.title')}
          actions={
            <TextField
              select
              size="small"
              label={t('filter.label')}
              value={sessionId}
              onChange={(e) => setSessionId(e.target.value)}
              slotProps={{ select: { native: true }, inputLabel: { shrink: true } }}
              sx={{ minInlineSize: '14rem' }}
            >
              <option value="">{t('filter.all')}</option>
              {(sessions.data ?? []).map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </TextField>
          }
        >
          {listError != null && <ErrorAlert error={listError} />}
          {recordings.data?.length === 0 && (
            <EmptyState
              titleAs="h3"
              icon={<GraphicEqOutlined />}
              title={sessionId ? t('empty.filteredTitle') : t('empty.title')}
              description={t('empty.body')}
            />
          )}
          {list.length > 0 && (
            <Box component="ul" sx={{ listStyle: 'none', margin: 0, padding: 0, display: 'grid' }}>
              {list.map((r) => (
                <RecordingRow
                  key={r.id}
                  recording={r}
                  sessionName={sessionNames.get(r.sessionId) ?? r.sessionId}
                />
              ))}
            </Box>
          )}
        </Panel>
      </Box>
    </AdminPage>
  )
}

interface RecordingRowProps {
  recording: Recording
  sessionName: string
}

function RecordingRow({ recording: r, sessionName }: RecordingRowProps) {
  const { t, i18n } = useTranslation('recordings')
  const locale = i18n.resolvedLanguage
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState(false)
  const remove = api.useMutation('delete', '/api/recordings/{recordingId}', {
    onSuccess: async () => {
      setConfirm(false)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['get', '/api/recordings'] }),
        queryClient.invalidateQueries({ queryKey: ['get', '/api/recordings/usage'] }),
      ])
    },
  })
  const removeError: unknown = remove.error
  const date = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' })
  const duration = durationOf(r)
  const inProgress = r.status === 'recording'
  const meta = [
    date.format(new Date(r.startedAt)),
    duration != null ? timecode(duration) : undefined,
    r.sizeBytes != null ? formatBytes(r.sizeBytes, locale) : undefined,
    r.languages.length > 0 ? r.languages.map((l) => l.toUpperCase()).join(' · ') : undefined,
  ].filter(Boolean)

  return (
    <Box
      component="li"
      sx={{
        display: 'grid',
        gap: 'var(--space-xs)',
        paddingBlock: 'var(--space-sm)',
        borderBlockStart: 'var(--rule-hair) solid var(--color-rule)',
        '&:first-of-type': { borderBlockStart: 'none', paddingBlockStart: 0 },
      }}
    >
      <Box
        sx={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          gap: 'var(--space-xs) var(--space-sm)',
        }}
      >
        <Box
          sx={{
            display: 'grid',
            gap: 'var(--space-3xs)',
            minInlineSize: 0,
            marginInlineEnd: 'auto',
          }}
        >
          <Typography
            component="h3"
            sx={{ fontWeight: 'var(--weight-strong)', overflowWrap: 'anywhere' }}
          >
            {r.title || sessionName}
          </Typography>
          <Typography variant="body2" sx={{ color: 'var(--color-neutral)' }}>
            {t('row.session', { name: sessionName })}
          </Typography>
        </Box>
        <StatusChip status={recordingChip[r.status]} label={t(`status.${r.status}`)} />
      </Box>
      <Typography
        variant="body2"
        sx={{ color: 'var(--color-muted)', fontVariantNumeric: 'tabular-nums' }}
      >
        {meta.join(' — ')}
      </Typography>
      {removeError != null && <ErrorAlert error={removeError} />}
      <Box sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-xs)' }}>
        {confirm ? (
          <>
            <Box component="span" sx={{ fontSize: 'var(--text-sm)', color: 'var(--color-danger)' }}>
              {t('row.deleteConfirm')}
            </Box>
            <Button
              variant="outlined"
              color="error"
              size="small"
              loading={remove.isPending}
              onClick={() => remove.mutate({ params: { path: { recordingId: r.id } } })}
            >
              {t('row.deleteYes')}
            </Button>
            <Button variant="text" color="secondary" size="small" onClick={() => setConfirm(false)}>
              {t('row.deleteNo')}
            </Button>
          </>
        ) : (
          <>
            <ButtonLink
              to="/replay/$id"
              params={{ id: r.sessionId }}
              search={{ recording: r.id }}
              variant="outlined"
              color="secondary"
              size="small"
              startIcon={<PlayArrowOutlined aria-hidden />}
            >
              {t('row.replay')}
            </ButtonLink>
            <Button
              component="a"
              href={audioUrl(r.id)}
              download={`${r.id}.m4a`}
              variant="outlined"
              color="secondary"
              size="small"
              startIcon={<DownloadOutlined aria-hidden />}
            >
              {t('row.audio')}
            </Button>
            <Button
              variant="outlined"
              color="error"
              size="small"
              disabled={inProgress}
              startIcon={<DeleteOutlined aria-hidden />}
              onClick={() => setConfirm(true)}
            >
              {t('row.delete')}
            </Button>
            {inProgress && (
              <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
                {t('row.deleteBusy')}
              </Typography>
            )}
          </>
        )}
      </Box>
    </Box>
  )
}
