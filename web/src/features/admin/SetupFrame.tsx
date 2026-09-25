// SPDX-License-Identifier: Apache-2.0
import ArrowBackOutlined from '@mui/icons-material/ArrowBackOutlined'
import ArrowForwardOutlined from '@mui/icons-material/ArrowForwardOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Typography from '@mui/material/Typography'
import { Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Panel } from '../../components/Panel'
import { ThemeToggle } from '../../components/ThemeToggle'
import { UiLanguageSwitcher } from '../../components/UiLanguageSwitcher'
import { Wordmark } from './AdminLayout'
import { setupSteps, type SetupStep } from './setupSteps'

export interface SetupFrameProps {
  step: SetupStep
  /** Step heading (h2). */
  heading: string
  intro?: ReactNode
  /** Show "Finish later" (a link to the admin); only once signed in. */
  canLeave?: boolean
  children: ReactNode
}

/**
 * The setup wizard's page: wordmark, UI language and theme switches (the
 * wizard's first choice), the page title, "Step n of 6" and the step.
 */
export function SetupFrame({ step, heading, intro, canLeave, children }: SetupFrameProps) {
  const { t } = useTranslation('setup')
  const n = setupSteps.indexOf(step) + 1
  return (
    <Box
      component="main"
      sx={{
        maxInlineSize: '40rem',
        marginInline: 'auto',
        paddingBlock: 'var(--space-xl)',
        paddingInline: 'var(--space-md)',
        display: 'grid',
        gap: 'var(--space-lg)',
      }}
    >
      <Box
        sx={{
          display: 'flex',
          flexWrap: 'wrap',
          justifyContent: 'space-between',
          alignItems: 'center',
          gap: 'var(--space-sm)',
        }}
      >
        <Wordmark />
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-xs)' }}>
          <UiLanguageSwitcher />
          <ThemeToggle />
        </Box>
      </Box>
      <Box
        sx={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'baseline',
          gap: 'var(--space-xs) var(--space-sm)',
        }}
      >
        <Typography variant="h2" component="h1" sx={{ marginInlineEnd: 'auto' }}>
          {t('title')}
        </Typography>
        {canLeave && (
          <Button component={Link} to="/admin" variant="text" color="secondary" size="small">
            {t('finishLater')}
          </Button>
        )}
      </Box>
      <Panel sx={{ padding: 'var(--space-lg)' }}>
        <Box sx={{ display: 'grid', gap: 'var(--space-2xs)' }}>
          <Typography variant="overline" component="p" sx={{ color: 'var(--color-muted)' }}>
            {t('stepOf', { n, total: setupSteps.length })}
          </Typography>
          <Typography variant="h4" component="h2">
            {heading}
          </Typography>
          {intro != null && (
            <Typography sx={{ color: 'var(--color-neutral)', maxInlineSize: 'var(--measure)' }}>
              {intro}
            </Typography>
          )}
        </Box>
        {children}
      </Panel>
    </Box>
  )
}

export interface StepActionsProps {
  onBack?: () => void
  onNext: () => void
  /** Label of the forward button; defaults to "Continue". */
  nextLabel?: string
  /** The forward button is the step's one filled button unless the step has its own. */
  nextVariant?: 'contained' | 'outlined' | 'text'
  nextLoading?: boolean
}

/** Back / Continue row at the end of a step. Continue is never blocked by a failed request. */
export function StepActions({
  onBack,
  onNext,
  nextLabel,
  nextVariant = 'contained',
  nextLoading,
}: StepActionsProps) {
  const { t } = useTranslation('setup')
  return (
    <Box
      sx={{
        display: 'flex',
        flexWrap: 'wrap',
        justifyContent: 'space-between',
        alignItems: 'center',
        gap: 'var(--space-xs)',
        paddingBlockStart: 'var(--space-sm)',
        borderBlockStart: 'var(--rule-hair) solid var(--color-rule)',
      }}
    >
      {onBack ? (
        <Button
          variant="text"
          color="secondary"
          startIcon={<ArrowBackOutlined aria-hidden />}
          onClick={onBack}
        >
          {t('back')}
        </Button>
      ) : (
        <span />
      )}
      <Button
        variant={nextVariant}
        color={nextVariant === 'contained' ? 'primary' : 'secondary'}
        size="large"
        endIcon={<ArrowForwardOutlined aria-hidden />}
        loading={nextLoading}
        loadingPosition="end"
        onClick={onNext}
      >
        {nextLabel ?? t('next')}
      </Button>
    </Box>
  )
}
