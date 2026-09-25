// SPDX-License-Identifier: Apache-2.0
import CheckOutlined from '@mui/icons-material/CheckOutlined'
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
import { Panel } from '../../components/Panel'
import type { ChipStatus } from '../../components/StatusChip'
import { StatusChip } from '../../components/StatusChip'
import { busy, useModelProgress } from './useModelProgress'

type LocalModel = Schemas['LocalModel']
type ModelStatus = LocalModel['status']
type Settings = Schemas['Settings']

/** Polling fallback while something downloads and /ws/admin isn't connected. */
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

const needsOllama = (m: LocalModel) => m.error?.code === ollamaUnreachable
const needed = (m: LocalModel) =>
  (m.status === 'missing' || m.status === 'error') && !needsOllama(m)
/** A whisper download that stopped part-way: the next start resumes it. */
const partial = (m: LocalModel) => m.status === 'missing' && (m.progress ?? 0) > 0

/** The settings field that picks this kind of model for new sessions. */
const selectedName = (s: Settings | undefined, kind: LocalModel['kind']) =>
  kind === 'whisper' ? s?.providers.local.whisperModel : s?.providers.local.gemmaModel

/**
 * The local model catalog (AI-13): download with live progress from
 * /ws/admin, and pick which ready model new local sessions use.
 */
export function ModelsPanel() {
  const { t, i18n } = useTranslation('models')
  const queryClient = useQueryClient()
  const socket = useModelProgress()
  const models = api.useQuery('get', '/api/models', undefined, {
    refetchInterval: (q) => (socket !== 'open' && q.state.data?.some(busy) ? pollMs : false),
  })
  const hw = api.useQuery('get', '/api/system/hardware')
  const settings = api.useQuery('get', '/api/settings')
  const [downloadError, setDownloadError] = useState<unknown>()
  const download = api.useMutation('post', '/api/models/{modelId}/download', {
    onMutate: () => setDownloadError(undefined),
    onError: (e) => setDownloadError(e),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['get', '/api/models'] }),
  })
  const [saved, setSaved] = useState<LocalModel>()
  const [choosing, setChoosing] = useState<string>()
  const save = api.useMutation('put', '/api/settings', {
    onMutate: () => setSaved(undefined),
    onSuccess: (data) => {
      queryClient.setQueryData(['get', '/api/settings'], data)
      void queryClient.invalidateQueries({ queryKey: ['get', '/api/settings'] })
      void queryClient.invalidateQueries({ queryKey: ['get', '/api/system/hardware'] })
    },
  })
  const modelsError: unknown = models.error
  const settingsError: unknown = settings.error
  const saveError: unknown = save.error

  const list = [...(models.data ?? [])].sort(
    (a, b) => a.kind.localeCompare(b.kind) || Number(!!b.recommended) - Number(!!a.recommended),
  )
  const missingRecommended = list.filter((m) => m.recommended && needed(m))
  const blocked = list.some(needsOllama)
  const whisperDown = hw.data ? !hw.data.runtimes.whisper.reachable : false
  const current = settings.data?.providers.local
  const inUse = (m: LocalModel) => selectedName(settings.data, m.kind) === m.name

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

  const start = (m: LocalModel) => download.mutate({ params: { path: { modelId: m.id } } })
  const use = (m: LocalModel) => {
    const s = settings.data
    if (!s) return
    const local = { ...s.providers.local }
    setChoosing(m.id)
    if (m.kind === 'whisper') local.whisperModel = m.name
    else local.gemmaModel = m.name
    save.mutate(
      { body: { ...s, providers: { ...s.providers, local } } },
      { onSuccess: () => setSaved(m) },
    )
  }
  const recheck = () => {
    void queryClient.invalidateQueries({ queryKey: ['get', '/api/models'] })
    void queryClient.invalidateQueries({ queryKey: ['get', '/api/system/hardware'] })
    void queryClient.invalidateQueries({ queryKey: ['get', '/api/settings'] })
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
    <Panel
      title={t('models.title')}
      actions={
        <>
          {missingRecommended.length > 0 && (
            <Button
              variant="outlined"
              color="secondary"
              size="small"
              startIcon={<DownloadOutlined aria-hidden />}
              onClick={() => missingRecommended.forEach(start)}
            >
              {t('models.downloadRecommended', { count: missingRecommended.length })}
            </Button>
          )}
          <Button
            variant="text"
            color="secondary"
            size="small"
            startIcon={<RefreshOutlined aria-hidden />}
            loading={models.isFetching && !models.isPending}
            loadingPosition="start"
            onClick={recheck}
          >
            {t('models.recheck')}
          </Button>
        </>
      }
    >
      <Typography variant="body2" sx={{ color: 'var(--color-neutral)' }}>
        {t('models.intro')}
      </Typography>
      {current && (
        <Typography>
          {t('models.current', { whisper: current.whisperModel, gemma: current.gemmaModel })}
        </Typography>
      )}
      {modelsError != null && <ErrorAlert error={modelsError} />}
      {downloadError != null && <ErrorAlert error={downloadError} />}
      {settingsError != null && (
        <>
          <Notice>{t('models.settingsUnavailable')}</Notice>
          <ErrorAlert error={settingsError} />
        </>
      )}
      {saveError != null && <ErrorAlert error={saveError} />}
      {saved && (
        <Notice>
          {saved.kind === 'whisper'
            ? t('models.savedWhisper', { name: saved.name })
            : t('models.savedGemma', { name: saved.name })}
        </Notice>
      )}
      {models.isPending && (
        <Typography sx={{ color: 'var(--color-muted)' }}>{t('models.loading')}</Typography>
      )}
      {(blocked || whisperDown) && (
        <Notice>
          {[
            whisperDown && t('models.whisperDown', { url: hw.data?.runtimes.whisper.url ?? '' }),
            blocked && t('models.ollamaDown', { url: hw.data?.runtimes.ollama.url ?? '' }),
            t('models.startSidecars'),
          ]
            .filter(Boolean)
            .join(' ')}
        </Notice>
      )}
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
            const selected = inUse(m)
            const ready = m.status === 'ready'
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
                    <Typography
                      sx={{ fontWeight: 'var(--weight-strong)', overflowWrap: 'anywhere' }}
                    >
                      {m.name}
                    </Typography>
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
                  {selected && (
                    <StatusChip
                      status={ready ? 'ok' : 'warn'}
                      label={ready ? t('models.inUse') : t('models.selectedMissing')}
                    />
                  )}
                  {!(selected && ready) && (
                    <StatusChip status={statusChip(m)} label={statusLabel(m)} />
                  )}
                  {ready && !selected && settings.data && (
                    <Button
                      variant="outlined"
                      color="secondary"
                      size="small"
                      startIcon={<CheckOutlined aria-hidden />}
                      loading={save.isPending && choosing === m.id}
                      disabled={save.isPending}
                      onClick={() => use(m)}
                    >
                      {t('models.use')}
                    </Button>
                  )}
                  {needed(m) && (
                    <Button
                      variant="outlined"
                      color="secondary"
                      size="small"
                      startIcon={<DownloadOutlined aria-hidden />}
                      disabled={
                        download.isPending && download.variables.params.path.modelId === m.id
                      }
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
    </Panel>
  )
}
