// SPDX-License-Identifier: Apache-2.0
import ExpandLessOutlined from '@mui/icons-material/ExpandLessOutlined'
import ExpandMoreOutlined from '@mui/icons-material/ExpandMoreOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Collapse from '@mui/material/Collapse'
import MuiLink from '@mui/material/Link'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Typography from '@mui/material/Typography'
import { createLink } from '@tanstack/react-router'
import { useId, useState } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import type { Schemas } from '../../api/types'
import { CopyField } from '../../components/CopyField'
import { segmentedSx } from '../../components/segmented'
import { StatusChip } from '../../components/StatusChip'
import { localRuntimesMissing } from '../admin/setupSteps'
import {
  guideOses,
  guideSteps,
  ollamaDefault,
  osFromReport,
  parseAddr,
  whisperDefault,
  type GuideOs,
} from './sidecarSteps'

const RouterLink = createLink(MuiLink)

const external = (href: string) => <MuiLink href={href} target="_blank" rel="noreferrer" />

/** The links the step texts may carry, as <brew>…</brew> tags. */
const stepLinks = {
  brew: external('https://brew.sh'),
  ollama: external('https://ollama.com/download'),
  releases: external('https://github.com/ggml-org/whisper.cpp/releases'),
}

export interface SidecarGuideProps {
  report: Schemas['HardwareReport']
  /** The speech model new sessions use; defaults to the recommendation. */
  whisperModel?: string
  /** The translation model new sessions use; defaults to the recommendation. */
  gemmaModel?: string
}

/**
 * How to install and start whisper-server and Ollama, per OS, with the
 * server's own addresses, models folder and models filled into each command.
 * Collapsed by default, so it never pushes a step's actions far down.
 */
export function SidecarGuide({ report, whisperModel, gemmaModel }: SidecarGuideProps) {
  const { t, i18n } = useTranslation('models')
  const [open, setOpen] = useState(false)
  const [os, setOs] = useState<GuideOs>(() => osFromReport(report.os))
  const panelId = useId()

  const missing = localRuntimesMissing(report)
  const running = (['whisper', 'ollama'] as const).filter((n) => !missing.includes(n))
  const name = (n: 'whisper' | 'ollama') => t(`hardware.runtime.${n}`)
  const whisper = whisperModel || report.recommendation.whisperModel
  const gemma = gemmaModel || report.recommendation.gemmaModel
  // What `brew install` gets, as a list in the UI language ("whisper.cpp and Ollama").
  const packages = new Intl.ListFormat(i18n.language, { type: 'conjunction' }).format(
    (missing.length ? missing : (['whisper', 'ollama'] as const)).map((n) =>
      n === 'whisper' ? 'whisper.cpp' : 'Ollama',
    ),
  )
  const steps = guideSteps({
    os,
    whisper: parseAddr(report.runtimes.whisper.url, whisperDefault),
    ollama: parseAddr(report.runtimes.ollama.url, ollamaDefault),
    modelsDir: report.modelsDir || t('guide.modelsDirPlaceholder'),
    whisperModel: whisper,
    gemmaModel: gemma,
    missing,
  })
  const usesModelsDir = missing.length === 0 || missing.includes('whisper')

  return (
    <Box sx={{ display: 'grid', gap: 'var(--space-sm)', justifyItems: 'start' }}>
      <Button
        variant="text"
        color="secondary"
        size="small"
        aria-expanded={open}
        aria-controls={panelId}
        startIcon={open ? <ExpandLessOutlined aria-hidden /> : <ExpandMoreOutlined aria-hidden />}
        onClick={() => setOpen((v) => !v)}
      >
        {missing.length
          ? t('guide.toggleMissing', { names: missing.map(name).join(' · ') })
          : t('guide.toggle')}
      </Button>
      <Collapse in={open} unmountOnExit sx={{ justifySelf: 'stretch' }}>
        <Box
          id={panelId}
          role="region"
          aria-label={t('guide.title')}
          sx={{
            display: 'grid',
            gap: 'var(--space-sm)',
            padding: 'var(--space-md)',
            borderRadius: 'var(--radius-card)',
            border: 'var(--rule-hair) solid var(--color-rule)',
          }}
        >
          <Typography variant="body2" sx={{ color: 'var(--color-neutral)' }}>
            {t('guide.intro')}
          </Typography>
          {missing.length === 0 ? (
            <Typography variant="body2" sx={{ color: 'var(--color-neutral)' }}>
              {t('guide.allRunning')}
            </Typography>
          ) : (
            running.length > 0 && (
              <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-xs)' }}>
                {running.map((n) => (
                  <StatusChip key={n} status="ok" label={t('guide.running', { name: name(n) })} />
                ))}
              </Box>
            )
          )}
          {/* In its own block so the grid doesn't stretch the tabs to full width. */}
          <Box>
            <ToggleButtonGroup
              exclusive
              value={os}
              onChange={(_, v: GuideOs | null) => v && setOs(v)}
              aria-label={t('guide.osLabel')}
              sx={segmentedSx}
            >
              {guideOses.map((o) => (
                <ToggleButton key={o} value={o}>
                  {t(`guide.os.${o}`)}
                </ToggleButton>
              ))}
            </ToggleButtonGroup>
          </Box>
          <Box
            component="ol"
            sx={{
              margin: 0,
              paddingInlineStart: 'var(--space-lg)',
              display: 'grid',
              gap: 'var(--space-sm)',
            }}
          >
            {steps.map((s) => (
              <Box
                component="li"
                key={s.key}
                data-sidecar={s.sidecar}
                sx={{ display: 'grid', gap: 'var(--space-xs)', minInlineSize: 0 }}
              >
                <Typography variant="body2">
                  <Trans
                    t={t}
                    i18nKey={`guide.step.${s.key}`}
                    values={{ whisper, gemma, packages }}
                    components={stepLinks}
                  />
                </Typography>
                {s.commands.map((c) => (
                  <CopyField key={c} value={c} variant="command" />
                ))}
                {(s.key === 'linux.build' || s.key === 'docker.whisper') && (
                  <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
                    {t(s.key === 'linux.build' ? 'guide.gpuLinux' : 'guide.gpu')}
                  </Typography>
                )}
              </Box>
            ))}
          </Box>
          {!report.modelsDir && usesModelsDir && (
            <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
              {t('guide.modelsDirHint')}
            </Typography>
          )}
          {os === 'docker' && (
            <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
              {t('guide.dockerMac')}
            </Typography>
          )}
          <Typography variant="body2" sx={{ color: 'var(--color-neutral)' }}>
            {t('guide.restart')}
          </Typography>
          <Typography variant="body2" sx={{ color: 'var(--color-neutral)' }}>
            {t('guide.addresses')}{' '}
            <RouterLink to="/admin/settings">{t('guide.openSettings')}</RouterLink>
          </Typography>
        </Box>
      </Collapse>
    </Box>
  )
}
