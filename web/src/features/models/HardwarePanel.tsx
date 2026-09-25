// SPDX-License-Identifier: Apache-2.0
import RefreshOutlined from '@mui/icons-material/RefreshOutlined'
import SpeedOutlined from '@mui/icons-material/SpeedOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Typography from '@mui/material/Typography'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import type { Schemas } from '../../api/types'
import { ErrorAlert } from '../../components/ErrorAlert'
import { Notice } from '../../components/Notice'
import { Panel } from '../../components/Panel'
import { Stat } from '../../components/Stat'
import { StatusChip } from '../../components/StatusChip'
import { localRuntimesMissing } from '../admin/setupSteps'
import { SidecarGuide } from './SidecarGuide'

type BenchmarkResult = Schemas['BenchmarkResult']
type RuntimeStatus = Schemas['RuntimeStatus']
type Num = (n: number, digits?: number) => string

const gib = 1024 ** 3

/** What this computer has, its sidecars, the recommendation and the benchmark (AI-12). */
export function HardwarePanel() {
  const { t, i18n } = useTranslation('models')
  const queryClient = useQueryClient()
  const hw = api.useQuery('get', '/api/system/hardware')
  // Shared with ModelsPanel on the same page: the models new sessions use.
  const local = api.useQuery('get', '/api/settings').data?.providers.local
  const bench = api.useMutation('post', '/api/system/benchmark', {
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['get', '/api/system/hardware'] }),
  })
  const hwError: unknown = hw.error
  const num: Num = (n, digits = 1) =>
    new Intl.NumberFormat(i18n.resolvedLanguage, { maximumFractionDigits: digits }).format(n)

  const report = hw.data
  const result: BenchmarkResult | undefined = bench.data ?? report?.lastBenchmark
  const missing = localRuntimesMissing(report)
  const runtimes = report
    ? ([
        ['whisper', report.runtimes.whisper],
        ['ollama', report.runtimes.ollama],
        ['ffmpeg', report.runtimes.ffmpeg],
      ] as const)
    : []
  const runtimeDetail = (r: RuntimeStatus) =>
    r.reachable
      ? r.version && t('hardware.version', { version: r.version })
      : r.url && t('hardware.notAnswering', { url: r.url })

  return (
    <Panel
      title={t('hardware.title')}
      actions={
        <Button
          variant="text"
          color="secondary"
          size="small"
          startIcon={<RefreshOutlined aria-hidden />}
          loading={hw.isFetching}
          loadingPosition="start"
          onClick={() => void hw.refetch()}
        >
          {t('hardware.recheck')}
        </Button>
      }
    >
      {hwError != null && <ErrorAlert error={hwError} />}
      {hw.isPending && (
        <Typography sx={{ color: 'var(--color-muted)' }}>{t('hardware.checking')}</Typography>
      )}
      {report && (
        <>
          <Box
            sx={{
              display: 'grid',
              gap: 'var(--space-md)',
              gridTemplateColumns: 'repeat(auto-fit, minmax(10rem, 1fr))',
            }}
          >
            <Stat label={t('hardware.system')} value={`${report.os} · ${report.arch}`} />
            <Stat
              label={t('hardware.cpu')}
              value={t('hardware.cores', { count: report.cpu.cores })}
              detail={report.cpu.model}
            />
            <Stat
              label={t('hardware.memory')}
              value={t('hardware.gb', { value: num(report.memoryBytes / gib) })}
            />
            <Stat
              label={t('hardware.gpu')}
              value={
                report.gpus.length ? report.gpus.map((g) => g.name).join(', ') : t('hardware.noGpu')
              }
              detail={report.gpus.map((g) => g.backend.toUpperCase()).join(', ') || undefined}
            />
          </Box>
          <Box sx={{ display: 'grid', gap: 'var(--space-xs)' }}>
            <Typography variant="overline" component="h3">
              {t('hardware.runtimes')}
            </Typography>
            <Box
              component="ul"
              sx={{
                listStyle: 'none',
                margin: 0,
                padding: 0,
                display: 'grid',
                gap: 'var(--space-xs)',
              }}
            >
              {runtimes.map(([name, r]) => {
                const detail = runtimeDetail(r)
                return (
                  <Box
                    component="li"
                    key={name}
                    sx={{
                      display: 'flex',
                      flexWrap: 'wrap',
                      alignItems: 'center',
                      gap: 'var(--space-2xs) var(--space-sm)',
                    }}
                  >
                    <StatusChip
                      status={r.reachable ? 'ok' : 'warn'}
                      label={t(r.reachable ? 'hardware.runtimeOk' : 'hardware.runtimeMissing', {
                        name: t(`hardware.runtime.${name}`),
                      })}
                    />
                    {detail && (
                      <Typography
                        variant="body2"
                        sx={{ color: 'var(--color-muted)', overflowWrap: 'anywhere' }}
                      >
                        {detail}
                      </Typography>
                    )}
                  </Box>
                )
              })}
            </Box>
          </Box>
          {missing.length > 0 && (
            <Notice>
              {t('hardware.localMissing', {
                names: missing.map((n) => t(`hardware.runtime.${n}`)).join(' · '),
              })}
            </Notice>
          )}
          <SidecarGuide
            report={report}
            whisperModel={local?.whisperModel}
            gemmaModel={local?.gemmaModel}
          />
          <Typography>
            {t('hardware.recommend', {
              whisper: report.recommendation.whisperModel,
              gemma: report.recommendation.gemmaModel,
            })}
          </Typography>
          {!report.recommendation.localRealtimeLikely && (
            <Notice>
              {report.lastBenchmark ? t('hardware.slowMeasured') : t('hardware.slow')}
            </Notice>
          )}
        </>
      )}

      <Box sx={{ display: 'grid', gap: 'var(--space-sm)', justifyItems: 'start' }}>
        <Typography variant="body2" sx={{ color: 'var(--color-neutral)' }}>
          {t('hardware.benchmarkHelp')}
        </Typography>
        <Button
          variant="outlined"
          color="secondary"
          startIcon={<SpeedOutlined aria-hidden />}
          loading={bench.isPending}
          loadingPosition="start"
          onClick={() => bench.mutate({})}
        >
          {bench.isPending ? t('hardware.benchmarkRunning') : t('hardware.benchmark')}
        </Button>
        {bench.error && (
          <Box sx={{ justifySelf: 'stretch' }}>
            <ErrorAlert error={bench.error} />
          </Box>
        )}
        {result && <BenchmarkReadout result={result} num={num} />}
      </Box>
    </Panel>
  )
}

function BenchmarkReadout({ result, num }: { result: BenchmarkResult; num: Num }) {
  const { t, i18n } = useTranslation('models')
  const models = [result.whisperModel, result.gemmaModel].filter(Boolean).join(' · ')
  const ranAt = new Intl.DateTimeFormat(i18n.resolvedLanguage, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(result.ranAt))
  return (
    <Box sx={{ display: 'grid', gap: 'var(--space-sm)', justifySelf: 'stretch' }}>
      <Box sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-md)' }}>
        <Stat
          label={t('hardware.rtf')}
          value={`${num(result.realTimeFactor, 2)}×`}
          detail={
            result.maxRealTimeFactor != null
              ? t('hardware.rtfLimit', { max: num(result.maxRealTimeFactor, 2) })
              : undefined
          }
        />
        <Stat
          label={t('hardware.timing')}
          value={t('hardware.rtfDetail', {
            asr: num(result.asrMs / 1000),
            translation: num(result.translationMs / 1000),
          })}
          detail={
            result.audioMs != null
              ? t('hardware.clip', { seconds: num(result.audioMs / 1000) })
              : undefined
          }
        />
        <StatusChip
          status={result.ok ? 'ok' : 'warn'}
          label={result.ok ? t('hardware.realtime') : t('hardware.notRealtime')}
        />
      </Box>
      <Typography>{result.ok ? t('hardware.verdictOk') : t('hardware.verdictSlow')}</Typography>
      <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
        {models ? t('hardware.ranWith', { models, at: ranAt }) : t('hardware.ranAt', { at: ranAt })}
      </Typography>
    </Box>
  )
}
