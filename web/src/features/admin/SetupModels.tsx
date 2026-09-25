// SPDX-License-Identifier: Apache-2.0
import DownloadOutlined from '@mui/icons-material/DownloadOutlined'
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
import type { ChipStatus } from '../../components/StatusChip'
import { StatusChip } from '../../components/StatusChip'
import { SetupFrame, StepActions } from './SetupFrame'

type LocalModel = Schemas['LocalModel']
type ModelStatus = LocalModel['status']

/** How often the list is polled while something downloads. */
const pollMs = 1500

const chip: Record<ModelStatus, ChipStatus> = {
  missing: 'idle',
  downloading: 'starting',
  verifying: 'starting',
  ready: 'ok',
  error: 'error',
}

const busy = (m: LocalModel) => m.status === 'downloading' || m.status === 'verifying'
const needed = (m: LocalModel) => m.status === 'missing' || m.status === 'error'

/** Step 4 (AI-13): download the local speech and translation models. */
export function SetupModels({ onBack, onNext }: { onBack: () => void; onNext: () => void }) {
  const { t, i18n } = useTranslation('setup')
  const queryClient = useQueryClient()
  const models = api.useQuery('get', '/api/models', undefined, {
    refetchInterval: (q) => (q.state.data?.some(busy) ? pollMs : false),
  })
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
  const size = (bytes?: number) =>
    bytes == null
      ? ''
      : t('models.size', {
          value: new Intl.NumberFormat(i18n.resolvedLanguage, { maximumFractionDigits: 1 }).format(
            bytes / 1024 ** 3,
          ),
        })
  const start = (m: LocalModel) => {
    setDownloadError(undefined)
    download.mutate({ params: { path: { modelId: m.id } } })
  }

  return (
    <SetupFrame step="models" heading={t('models.heading')} intro={t('models.intro')} canLeave>
      {modelsError != null && <ErrorAlert error={modelsError} />}
      {downloadError != null && <ErrorAlert error={downloadError} />}
      {models.isPending && (
        <Typography sx={{ color: 'var(--color-muted)' }}>{t('models.loading')}</Typography>
      )}
      {missingRecommended.length > 0 && (
        <Button
          variant="outlined"
          color="secondary"
          startIcon={<DownloadOutlined aria-hidden />}
          onClick={() => missingRecommended.forEach(start)}
          sx={{ justifySelf: 'start' }}
        >
          {t('models.downloadRecommended', { count: missingRecommended.length })}
        </Button>
      )}
      {models.isSuccess && list.length === 0 && (
        <Typography sx={{ color: 'var(--color-muted)' }}>{t('models.none')}</Typography>
      )}
      {list.length > 0 && (
        <Box
          component="ul"
          sx={{ listStyle: 'none', margin: 0, padding: 0, display: 'grid', gap: 'var(--space-sm)' }}
        >
          {list.map((m) => (
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
                      size(m.sizeBytes),
                      m.recommended ? t('models.recommended') : '',
                    ]
                      .filter(Boolean)
                      .join(' · ')}
                  </Typography>
                </Box>
                <StatusChip status={chip[m.status]} label={t(`models.status.${m.status}`)} />
                {needed(m) && (
                  <Button
                    variant="outlined"
                    color="secondary"
                    size="small"
                    startIcon={<DownloadOutlined aria-hidden />}
                    onClick={() => start(m)}
                  >
                    {m.status === 'error' ? t('models.retry') : t('models.download')}
                  </Button>
                )}
              </Box>
              {busy(m) && (
                <LinearProgress
                  variant={m.progress != null ? 'determinate' : 'indeterminate'}
                  value={(m.progress ?? 0) * 100}
                  aria-label={t('models.progress', { name: m.name })}
                />
              )}
              {m.status === 'error' && m.error && <ErrorAlert error={m.error} />}
            </Box>
          ))}
        </Box>
      )}
      <Typography variant="body2" sx={{ color: 'var(--color-neutral)' }}>
        {t('models.background')}
      </Typography>
      <StepActions
        onBack={onBack}
        onNext={onNext}
        nextLabel={modelsError != null ? t('skip') : undefined}
      />
    </SetupFrame>
  )
}
