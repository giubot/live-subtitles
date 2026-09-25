// SPDX-License-Identifier: Apache-2.0
import DownloadOutlined from '@mui/icons-material/DownloadOutlined'
import RefreshOutlined from '@mui/icons-material/RefreshOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import LinearProgress from '@mui/material/LinearProgress'
import Typography from '@mui/material/Typography'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import type { Schemas } from '../../api/types'
import { ErrorAlert } from '../../components/ErrorAlert'
import { Notice } from '../../components/Notice'
import type { ChipStatus } from '../../components/StatusChip'
import { StatusChip } from '../../components/StatusChip'
import { SidecarGuide } from '../models/SidecarGuide'
import { SetupFrame, StepActions } from './SetupFrame'

type LocalModel = Schemas['LocalModel']
type ModelStatus = LocalModel['status']

/** How often the list is polled while something downloads (GET /api/models shows progress). */
const pollMs = 1500

/** The Gemma models are pulled through Ollama; without it they can't download. */
const ollamaUnreachable = 'model.ollama_unreachable'

const chip: Record<ModelStatus, ChipStatus> = {
  missing: 'idle',
  downloading: 'starting',
  verifying: 'starting',
  ready: 'ok',
  error: 'error',
}

const busy = (m: LocalModel) => m.status === 'downloading' || m.status === 'verifying'
const needsOllama = (m: LocalModel) => m.error?.code === ollamaUnreachable
const needed = (m: LocalModel) =>
  (m.status === 'missing' || m.status === 'error') && !needsOllama(m)
/** A whisper download that stopped part-way: the next start resumes it. */
const partial = (m: LocalModel) => m.status === 'missing' && (m.progress ?? 0) > 0

/**
 * Step 3 (AI-13): download the local speech and translation models. The
 * server keeps downloading in the background, resumes a whisper file that
 * stopped part-way, and needs Ollama running to pull the Gemma models.
 */
export function SetupModels({ onBack, onNext }: { onBack: () => void; onNext: () => void }) {
  const { t, i18n } = useTranslation('setup')
  const queryClient = useQueryClient()
  const models = api.useQuery('get', '/api/models', undefined, {
    refetchInterval: (q) => (q.state.data?.some(busy) ? pollMs : false),
  })
  const hw = api.useQuery('get', '/api/system/hardware')
  const [downloadError, setDownloadError] = useState<unknown>()
  const download = api.useMutation('post', '/api/models/{modelId}/download', {
    onError: (e) => setDownloadError(e),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['get', '/api/models'] }),
  })
  const modelsError: unknown = models.error

  const list = [...(models.data ?? [])].sort(
    (a, b) => Number(!!b.recommended) - Number(!!a.recommended),
  )
  const missingRecommended = list.filter((m) => m.recommended && needed(m))
  const blocked = list.some(needsOllama)
  const whisperDown = hw.data ? !hw.data.runtimes.whisper.reachable : false
  const ollamaUrl = hw.data?.runtimes.ollama.url
  const whisperUrl = hw.data?.runtimes.whisper.url

  const lang = i18n.resolvedLanguage
  const bytes = (n: number) => {
    const gb = n >= 1e9
    return new Intl.NumberFormat(lang, {
      style: 'unit',
      unit: gb ? 'gigabyte' : 'megabyte',
      maximumFractionDigits: gb ? 1 : 0,
    }).format(gb ? n / 1e9 : n / 1e6)
  }
  const percent = (p: number) =>
    new Intl.NumberFormat(lang, { style: 'percent', maximumFractionDigits: 0 }).format(p)
  const start = (m: LocalModel) => {
    setDownloadError(undefined)
    download.mutate({ params: { path: { modelId: m.id } } })
  }
  const recheck = () => {
    void queryClient.invalidateQueries({ queryKey: ['get', '/api/models'] })
    void queryClient.invalidateQueries({ queryKey: ['get', '/api/system/hardware'] })
  }

  const statusLabel = (m: LocalModel) => {
    if (needsOllama(m)) return t('models.needsOllama')
    if (partial(m)) return t('models.paused', { percent: percent(m.progress ?? 0) })
    return t(`models.status.${m.status}`)
  }
  const statusChip = (m: LocalModel): ChipStatus =>
    needsOllama(m) || partial(m) ? 'warn' : chip[m.status]
  const detail = (m: LocalModel) => {
    if (m.status === 'downloading' && m.progress != null && m.sizeBytes)
      return t('models.progressOf', {
        done: bytes(m.progress * m.sizeBytes),
        total: bytes(m.sizeBytes),
        percent: percent(m.progress),
      })
    if (m.status === 'verifying') return t('models.verifying')
    return undefined
  }

  return (
    <SetupFrame step="models" heading={t('models.heading')} intro={t('models.intro')} canLeave>
      {modelsError != null && <ErrorAlert error={modelsError} />}
      {downloadError != null && <ErrorAlert error={downloadError} />}
      {models.isPending && (
        <Typography sx={{ color: 'var(--color-muted)' }}>{t('models.loading')}</Typography>
      )}
      {(blocked || whisperDown) && (
        <Notice>
          {[
            whisperDown && t('models.whisperDown', { url: whisperUrl ?? '' }),
            blocked && t('models.ollamaDown', { url: ollamaUrl ?? '' }),
            t('models.startSidecars'),
          ]
            .filter(Boolean)
            .join(' ')}
        </Notice>
      )}
      {hw.data && <SidecarGuide report={hw.data} />}
      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-xs)' }}>
        {missingRecommended.length > 0 && (
          <Button
            variant="outlined"
            color="secondary"
            startIcon={<DownloadOutlined aria-hidden />}
            onClick={() => missingRecommended.forEach(start)}
          >
            {t('models.downloadRecommended', { count: missingRecommended.length })}
          </Button>
        )}
        <Button
          variant="text"
          color="secondary"
          startIcon={<RefreshOutlined aria-hidden />}
          loading={models.isFetching && !models.isPending}
          loadingPosition="start"
          onClick={recheck}
        >
          {t('models.recheck')}
        </Button>
      </Box>
      {models.isSuccess && list.length === 0 && (
        <Typography sx={{ color: 'var(--color-muted)' }}>{t('models.none')}</Typography>
      )}
      {list.length > 0 && (
        <Box
          component="ul"
          sx={{ listStyle: 'none', margin: 0, padding: 0, display: 'grid', gap: 'var(--space-sm)' }}
        >
          {list.map((m) => {
            const line = detail(m)
            return (
              <Box
                component="li"
                key={m.id}
                sx={{
                  display: 'grid',
                  gap: 'var(--space-xs)',
                  paddingBlockEnd: 'var(--space-sm)',
                  borderBlockEnd: 'var(--rule-hair) solid var(--color-rule)',
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
                  <Box sx={{ marginInlineEnd: 'auto', minInlineSize: 0 }}>
                    <Typography sx={{ fontWeight: 'var(--weight-strong)' }}>{m.name}</Typography>
                    <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
                      {[
                        t(`models.kind.${m.kind}`),
                        m.sizeBytes != null ? bytes(m.sizeBytes) : '',
                        m.recommended ? t('models.recommended') : '',
                      ]
                        .filter(Boolean)
                        .join(' · ')}
                    </Typography>
                  </Box>
                  <StatusChip status={statusChip(m)} label={statusLabel(m)} />
                  {needed(m) && (
                    <Button
                      variant="outlined"
                      color="secondary"
                      size="small"
                      startIcon={<DownloadOutlined aria-hidden />}
                      onClick={() => start(m)}
                    >
                      {partial(m)
                        ? t('models.resume')
                        : m.status === 'error'
                          ? t('models.retry')
                          : t('models.download')}
                    </Button>
                  )}
                </Box>
                {(busy(m) || partial(m)) && (
                  <LinearProgress
                    variant={
                      m.progress != null && m.status !== 'verifying'
                        ? 'determinate'
                        : 'indeterminate'
                    }
                    value={(m.progress ?? 0) * 100}
                    aria-label={t('models.progress', { name: m.name })}
                  />
                )}
                {line && (
                  <Typography
                    variant="body2"
                    sx={{ color: 'var(--color-muted)', fontVariantNumeric: 'tabular-nums' }}
                  >
                    {line}
                  </Typography>
                )}
                {m.status === 'error' && m.error && !needsOllama(m) && (
                  <ErrorAlert error={m.error} />
                )}
              </Box>
            )
          })}
        </Box>
      )}
      <Typography variant="body2" sx={{ color: 'var(--color-neutral)' }}>
        {t('models.background')}
      </Typography>
      <StepActions onBack={onBack} onNext={onNext} />
    </SetupFrame>
  )
}
