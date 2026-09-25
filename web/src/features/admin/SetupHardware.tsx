// SPDX-License-Identifier: Apache-2.0
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
import { SetupFrame, StepActions } from './SetupFrame'

type BenchmarkResult = Schemas['BenchmarkResult']

const gib = 1024 ** 3

/** Step 3 (AI-12): what this computer has, what it should run, and a benchmark. */
export function SetupHardware({ onNext }: { onNext: () => void }) {
  const { t, i18n } = useTranslation('setup')
  const queryClient = useQueryClient()
  const hw = api.useQuery('get', '/api/system/hardware')
  const bench = api.useMutation('post', '/api/system/benchmark', {
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['get', '/api/system/hardware'] }),
  })
  const hwError: unknown = hw.error
  const num = (n: number, digits = 1) =>
    new Intl.NumberFormat(i18n.resolvedLanguage, { maximumFractionDigits: digits }).format(n)

  const report = hw.data
  const result: BenchmarkResult | undefined = bench.data ?? report?.lastBenchmark
  const runtimes = report
    ? ([
        ['whisper', report.runtimes.whisper],
        ['ollama', report.runtimes.ollama],
        ['ffmpeg', report.runtimes.ffmpeg],
      ] as const)
    : []

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
            <Typography variant="overline" component="h3">
              {t('hardware.runtimes')}
            </Typography>
            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-xs)' }}>
              {runtimes.map(([name, r]) => (
                <StatusChip
                  key={name}
                  status={r.reachable ? 'ok' : 'warn'}
                  label={t(r.reachable ? 'hardware.runtimeOk' : 'hardware.runtimeMissing', {
                    name: t(`hardware.runtime.${name}`),
                  })}
                />
              ))}
            </Box>
          </Box>
          <Typography>
            {t('hardware.recommend', {
              whisper: report.recommendation.whisperModel,
              gemma: report.recommendation.gemmaModel,
            })}
          </Typography>
          {!report.recommendation.localRealtimeLikely && <Notice>{t('hardware.slow')}</Notice>}
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
          {t('hardware.benchmark')}
        </Button>
        {bench.error && <ErrorAlert error={bench.error} sx={{ justifySelf: 'stretch' }} />}
        {result && (
          <Box
            sx={{
              display: 'flex',
              flexWrap: 'wrap',
              alignItems: 'center',
              gap: 'var(--space-md)',
            }}
          >
            <Stat
              label={t('hardware.rtf')}
              value={`${num(result.realTimeFactor, 2)}×`}
              detail={t('hardware.rtfDetail', {
                asr: num(result.asrMs, 0),
                translation: num(result.translationMs, 0),
              })}
            />
            <StatusChip
              status={result.ok && result.realTimeFactor < 1 ? 'ok' : 'warn'}
              label={
                result.ok && result.realTimeFactor < 1
                  ? t('hardware.realtime')
                  : t('hardware.notRealtime')
              }
            />
          </Box>
        )}
      </Box>

      <StepActions onNext={onNext} nextLabel={hwError != null ? t('skip') : undefined} />
    </SetupFrame>
  )
}
