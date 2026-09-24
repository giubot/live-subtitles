// SPDX-License-Identifier: Apache-2.0
import CheckCircleOutlined from '@mui/icons-material/CheckCircleOutlined'
import ContentCopyOutlined from '@mui/icons-material/ContentCopyOutlined'
import ErrorOutlineOutlined from '@mui/icons-material/ErrorOutlineOutlined'
import PlayArrowOutlined from '@mui/icons-material/PlayArrowOutlined'
import StopOutlined from '@mui/icons-material/StopOutlined'
import AppBar from '@mui/material/AppBar'
import Box from '@mui/material/Box'
import Button, { type ButtonProps } from '@mui/material/Button'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import Container from '@mui/material/Container'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Toolbar from '@mui/material/Toolbar'
import Typography from '@mui/material/Typography'
import { createFileRoute, notFound } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ThemeToggle } from '../../../components/ThemeToggle'
import { Tooltip } from '../../../components/Tooltip'
import { UiLanguageSwitcher } from '../../../components/UiLanguageSwitcher'

// Dev-only reference page for the design system (docs/design.md). P1-12 adds
// the shared primitives here in all eight states.
export const Route = createFileRoute('/_themed/dev/design')({
  beforeLoad: () => {
    if (!import.meta.env.DEV) throw notFound()
  },
  component: DesignPage,
})

const colours = [
  'paper',
  'paper-2',
  'paper-3',
  'rule',
  'rule-2',
  'control',
  'ink',
  'ink-2',
  'neutral',
  'muted',
  'accent',
  'accent-hover',
  'accent-text',
  'accent-soft',
  'focus',
  'live',
  'ok',
  'ok-soft',
  'warn',
  'warn-soft',
  'warn-fill',
  'danger',
  'danger-soft',
  'graphite',
  'graphite-ink',
]

const typeScale = [
  ['h1', '--text-2xl'],
  ['h2', '--text-xl'],
  ['h3', '--text-lg'],
  ['h5', '--text-md'],
  ['body1', '--text-base'],
  ['body2', '--text-sm'],
  ['overline', '--text-xs'],
] as const

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Box component="section" sx={{ display: 'grid', gap: 'var(--space-md)' }}>
      <Typography variant="h5" component="h2">
        {title}
      </Typography>
      {children}
    </Box>
  )
}

function Mono({ children }: { children: ReactNode }) {
  return (
    <Box
      component="code"
      sx={{
        fontFamily: 'var(--font-mono)',
        fontSize: 'var(--text-sm)',
        color: 'var(--color-muted)',
      }}
    >
      {children}
    </Box>
  )
}

function DesignPage() {
  const { t } = useTranslation('dev')
  const states = ['default', 'focus', 'disabled', 'loading'] as const
  const buttons: { label: string; props: ButtonProps; icon: ReactNode }[] = [
    {
      label: 'contained · primary',
      props: { variant: 'contained' },
      icon: <PlayArrowOutlined aria-hidden />,
    },
    {
      label: 'outlined · secondary',
      props: { variant: 'outlined', color: 'secondary' },
      icon: <ContentCopyOutlined aria-hidden />,
    },
    {
      label: 'outlined · error',
      props: { variant: 'outlined', color: 'error' },
      icon: <StopOutlined aria-hidden />,
    },
    {
      label: 'text · primary',
      props: { variant: 'text' },
      icon: <ContentCopyOutlined aria-hidden />,
    },
  ]
  const buttonText = [t('sample.start'), t('sample.cancel'), t('sample.stop'), t('sample.copy')]

  return (
    <>
      <AppBar>
        <Toolbar sx={{ gap: 'var(--space-sm)', flexWrap: 'wrap' }}>
          <Typography variant="h5" component="h1" sx={{ flexGrow: 1 }}>
            {t('title')}
          </Typography>
          <UiLanguageSwitcher />
          <ThemeToggle />
        </Toolbar>
      </AppBar>
      <Container
        component="main"
        maxWidth="lg"
        sx={{ py: 'var(--space-xl)', display: 'grid', gap: 'var(--space-2xl)' }}
      >
        <Typography sx={{ maxWidth: 'var(--measure)' }}>{t('intro')}</Typography>

        <Section title={t('colour')}>
          <Box
            sx={{
              display: 'grid',
              gap: 'var(--space-sm)',
              gridTemplateColumns: 'repeat(auto-fill, minmax(10rem, 1fr))',
            }}
          >
            {colours.map((c) => (
              <Stack key={c} spacing={0.5}>
                <Box
                  sx={{
                    height: 'var(--space-xl)',
                    borderRadius: 'var(--radius-chip)',
                    border: 'var(--rule-hair) solid var(--color-rule-2)',
                    background: `var(--color-${c})`,
                  }}
                />
                <Mono>--color-{c}</Mono>
              </Stack>
            ))}
          </Box>
        </Section>

        <Section title={t('type')}>
          {typeScale.map(([variant, token]) => (
            <Stack key={variant} direction="row" spacing={2} sx={{ alignItems: 'baseline' }}>
              <Box sx={{ minWidth: '8rem' }}>
                <Mono>{token}</Mono>
              </Box>
              <Typography variant={variant}>{t('sample.card')}</Typography>
            </Stack>
          ))}
          <Mono>00:12:34.200 · 192.168.1.20:8080 · srt://:9000</Mono>
        </Section>

        <Section title={t('buttons')}>
          <Box
            sx={{
              display: 'grid',
              gap: 'var(--space-sm)',
              // A fixed table (variant × state) that scrolls inside its own box on narrow screens.
              gridTemplateColumns: 'repeat(5, max-content)',
              alignItems: 'center',
              overflowX: 'auto',
              paddingBlock: 'var(--space-2xs)',
            }}
          >
            <span />
            {states.map((s) => (
              <Typography key={s} variant="overline">
                {t(`state.${s}`)}
              </Typography>
            ))}
            {buttons.map(({ label, props, icon }, i) => (
              <Box key={label} sx={{ display: 'contents' }}>
                <Mono>{label}</Mono>
                <Button {...props} startIcon={icon}>
                  {buttonText[i]}
                </Button>
                <Button {...props} startIcon={icon} className="Mui-focusVisible">
                  {buttonText[i]}
                </Button>
                <Tooltip title={t('sample.reason')}>
                  <span>
                    <Button {...props} startIcon={icon} disabled>
                      {buttonText[i]}
                    </Button>
                  </span>
                </Tooltip>
                <Button {...props} startIcon={icon} loading loadingPosition="start">
                  {buttonText[i]}
                </Button>
              </Box>
            ))}
          </Box>
          <Tooltip title={t('sample.tooltip')}>
            <Button variant="outlined" color="secondary" sx={{ justifySelf: 'start' }}>
              {t('sample.copy')}
            </Button>
          </Tooltip>
        </Section>

        <Section title={t('inputs')}>
          <Box
            sx={{
              display: 'grid',
              gap: 'var(--space-md)',
              gridTemplateColumns: 'repeat(auto-fit, minmax(14rem, 1fr))',
            }}
          >
            <TextField
              label={t('sample.sessionName')}
              helperText={t('sample.sessionHelper')}
              defaultValue={t('sample.sessionValue')}
            />
            <TextField
              label={t('sample.sessionName')}
              error
              helperText={t('sample.sessionError')}
              defaultValue={t('sample.sessionValue')}
              slotProps={{ htmlInput: { 'aria-invalid': true } }}
            />
            <TextField
              label={t('sample.sessionName')}
              disabled
              helperText={t('sample.reason')}
              defaultValue={t('sample.sessionValue')}
            />
            <TextField
              label={t('sample.sessionName')}
              defaultValue={t('sample.sessionValue')}
              color="success"
              focused
              helperText={
                <Box
                  component="span"
                  sx={{
                    display: 'inline-flex',
                    gap: 'var(--space-2xs)',
                    alignItems: 'center',
                    color: 'var(--color-ok)',
                  }}
                >
                  <CheckCircleOutlined aria-hidden fontSize="inherit" /> {t('sample.saved')}
                </Box>
              }
            />
          </Box>
        </Section>

        <Section title={t('surfaces')}>
          <Box
            sx={{
              display: 'grid',
              gap: 'var(--space-md)',
              gridTemplateColumns: 'repeat(auto-fit, minmax(16rem, 1fr))',
            }}
          >
            <Card>
              <CardContent>
                <Typography variant="h5" component="h3">
                  {t('sample.card')}
                </Typography>
                <Typography variant="body2" color="text.secondary">
                  {t('sample.cardBody')}
                </Typography>
              </CardContent>
            </Card>
            <Box
              sx={{
                background: 'var(--color-graphite)',
                color: 'var(--color-graphite-ink)',
                borderRadius: 'var(--radius-card)',
                p: 'var(--space-md)',
                display: 'grid',
                gap: 'var(--space-xs)',
              }}
            >
              <Typography variant="overline">{t('sample.copy')}</Typography>
              <Box
                component="code"
                sx={{ fontFamily: 'var(--font-mono)', overflowWrap: 'anywhere' }}
              >
                http://192.168.1.20:8080/overlay/main-stage?lang=en
              </Box>
            </Box>
            <Box
              sx={{
                border: 'var(--rule-hair) solid var(--color-danger)',
                background: 'var(--color-danger-soft)',
                color: 'var(--color-danger)',
                borderRadius: 'var(--radius-card)',
                p: 'var(--space-md)',
                display: 'flex',
                gap: 'var(--space-xs)',
              }}
            >
              <ErrorOutlineOutlined aria-hidden />
              <Typography>{t('sample.sessionError')}</Typography>
            </Box>
          </Box>
        </Section>

        <Section title={t('captions')}>
          <Box
            sx={{
              display: 'grid',
              gap: 'var(--space-md)',
              gridTemplateColumns: 'repeat(auto-fit, minmax(16rem, 1fr))',
            }}
          >
            <Box
              lang="en"
              sx={{
                fontSize: 'var(--text-caption)',
                fontWeight: 'var(--weight-caption)',
                lineHeight: 'var(--leading-caption)',
              }}
            >
              <Box sx={{ color: 'var(--color-ink)' }}>{t('sample.final')}</Box>
              <Box sx={{ color: 'var(--color-muted)' }}>
                {t('sample.interim')}
                <Box component="span" aria-hidden sx={{ color: 'var(--color-accent)' }}>
                  ▍
                </Box>
              </Box>
            </Box>
            {[
              ['--stage-bg-dark', '--stage-fg-white'],
              ['--stage-bg-dark', '--stage-fg-yellow'],
              ['--stage-bg-light', '--stage-fg-black'],
            ].map(([bg, fg]) => (
              <Box
                key={fg}
                sx={{
                  background: `var(${bg})`,
                  color: `var(${fg})`,
                  borderRadius: 'var(--radius-card)',
                  p: 'var(--space-lg)',
                  fontSize: 'var(--text-md)',
                  fontWeight: 'var(--weight-caption)',
                }}
              >
                {t('sample.final')}
              </Box>
            ))}
            <Box
              sx={{
                background: 'var(--color-graphite)',
                borderRadius: 'var(--radius-card)',
                p: 'var(--space-lg)',
                display: 'grid',
                placeItems: 'end center',
                minHeight: '8rem',
              }}
            >
              <Box
                component="span"
                sx={{
                  background: 'var(--overlay-box)',
                  color: 'var(--overlay-fg)',
                  px: 'var(--space-xs)',
                  fontWeight: 600,
                }}
              >
                {t('sample.final')}
              </Box>
            </Box>
          </Box>
        </Section>
      </Container>
    </>
  )
}
