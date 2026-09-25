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
import { Stat } from '../../components/Stat'
import { StatusChip } from '../../components/StatusChip'
import { SidecarGuide } from '../models/SidecarGuide'
import { SetupFrame, StepActions } from './SetupFrame'
import { localRuntimesMissing } from './setupSteps'

type BenchmarkResult = Schemas['BenchmarkResult']
type RuntimeStatus = Schemas['RuntimeStatus']

const gib = 1024 ** 3

/**
 * Step 2 (AI-12): what this computer has, what it should run, and a
 * benchmark of the local provider. Without whisper-server and Ollama the
 * benchmark can't run (422 `benchmark.runtime_unavailable`): the step says
 * so up front and carries on.
 */
export function SetupHardware({ onNext }: { onNext: () => void }) {
  const { t, i18n } = useTranslation('setup')
  const queryClient = useQueryClient()
  const hw = api.useQuery('get', '/api/system/hardware')
  const bench = api.useMutation('post', '/api/system/benchmark', {
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['get', '/api/system/hardware'] }),
  })
  const hwError: unknown = hw.error
  const num = (n: number, digits = 1) =>
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
    <SetupFrame
      step="hardware"
      heading={t('hardware.heading')}
      intro={t('hardware.intro')}
      canLeave
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
            <Box
              sx={{
                display: 'flex',
                flexWrap: 'wrap',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 'var(--space-xs)',
              }}
            >
              <Typography variant="overline" component="h3">
                {t('hardware.runtimes')}
              </Typography>
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
            </Box>
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
          <SidecarGuide report={report} />
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
          <Box sx={{ display: 'grid', gap: 'var(--space-2xs)', justifySelf: 'stretch' }}>
            <ErrorAlert error={bench.error} />
            <Typography variant="body2" sx={{ color: 'var(--color-neutral)' }}>
              {t('hardware.benchmarkSkip')}
            </Typography>
          </Box>
        )}
        {result && <BenchmarkReadout result={result} num={num} />}
      </Box>

      <StepActions onNext={onNext} nextLabel={hwError != null ? t('skip') : undefined} />
    </SetupFrame>
  )
}

function BenchmarkReadout({
  result,
  num,
}: {
  result: BenchmarkResult
  num: (n: number, digits?: number) => string
}) {
  const { t, i18n } = useTranslation('setup')
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
      <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
        {models ? t('hardware.ranWith', { models, at: ranAt }) : t('hardware.ranAt', { at: ranAt })}
      </Typography>
    </Box>
  )
}
