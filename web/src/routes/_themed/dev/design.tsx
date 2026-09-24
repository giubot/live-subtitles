// SPDX-License-Identifier: Apache-2.0
import AddOutlined from '@mui/icons-material/AddOutlined'
import CheckCircleOutlined from '@mui/icons-material/CheckCircleOutlined'
import ContentCopyOutlined from '@mui/icons-material/ContentCopyOutlined'
import ErrorOutlineOutlined from '@mui/icons-material/ErrorOutlineOutlined'
import PauseOutlined from '@mui/icons-material/PauseOutlined'
import PlayArrowOutlined from '@mui/icons-material/PlayArrowOutlined'
import RefreshOutlined from '@mui/icons-material/RefreshOutlined'
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
import Slider from '@mui/material/Slider'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import { createFileRoute, notFound } from '@tanstack/react-router'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { CopyField } from '../../../components/CopyField'
import { EmptyState } from '../../../components/EmptyState'
import { ErrorAlert } from '../../../components/ErrorAlert'
import { KbdHint } from '../../../components/KbdHint'
import { LanguagePicker } from '../../../components/LanguagePicker'
import { LevelMeter } from '../../../components/LevelMeter'
import { Panel } from '../../../components/Panel'
import { QrCode } from '../../../components/QrCode'
import { Stat } from '../../../components/Stat'
import { StatusChip, type ChipStatus } from '../../../components/StatusChip'
import { ThemeToggle } from '../../../components/ThemeToggle'
import { Tooltip } from '../../../components/Tooltip'
import { UiLanguageSwitcher } from '../../../components/UiLanguageSwitcher'
import { nativeLanguageName } from '../../../components/languageNames'
import { segmentedSx } from '../../../components/segmented'

// Dev-only reference page for the design system (docs/design.md), with the
// shared primitives from web/src/components in their states (P1-12).
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

/** One labelled cell in a state gallery. */
function State({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Box sx={{ display: 'grid', gap: 'var(--space-xs)', justifyItems: 'start', minWidth: 0 }}>
      <Typography variant="overline" sx={{ color: 'var(--color-muted)' }}>
        {label}
      </Typography>
      {children}
    </Box>
  )
}

function Gallery({ min = '11rem', children }: { min?: string; children: ReactNode }) {
  return (
    <Box
      sx={{
        display: 'grid',
        gap: 'var(--space-md) var(--space-lg)',
        gridTemplateColumns: `repeat(auto-fill, minmax(min(${min}, 100%), 1fr))`,
        alignItems: 'start',
      }}
    >
      {children}
    </Box>
  )
}

const overlayUrl = 'http://192.168.1.20:8080/overlay/main-stage?lang=es&preset=classic'
const captureUrl = 'http://192.168.1.20:8080/capture/main-stage?token=cap_7Qm2x9Lr4Vt8'
const viewerUrl = 'http://192.168.1.20:8080/s/main-stage'

/** The shared primitives from web/src/components in their states (P1-12). */
function Primitives() {
  const { t } = useTranslation('dev')
  const [level, setLevel] = useState(-14)
  const [lang, setLang] = useState('es')
  const [seg, setSeg] = useState('es')
  const chips: { status: ChipStatus; label?: string }[] = [
    { status: 'live' },
    { status: 'ok', label: t('sample.connected') },
    { status: 'idle' },
    { status: 'warn', label: t('sample.paused') },
    { status: 'starting' },
    { status: 'error' },
  ]
  const meters: [string, number][] = [
    [t('state.silent'), -Infinity],
    [t('state.default'), -30],
    [t('state.hot'), -8],
    [t('state.clipping'), -0.5],
  ]
  const segOptions = [
    { v: 'es', name: nativeLanguageName('es') },
    { v: 'en', name: nativeLanguageName('en') },
    { v: 'pt', name: nativeLanguageName('pt') },
  ]

  return (
    <>
      <Typography sx={{ maxWidth: 'var(--measure)', color: 'var(--color-neutral)' }}>
        {t('primitives.intro')}
      </Typography>

      <Section title={t('primitives.status')}>
        <Stack direction="row" useFlexGap sx={{ flexWrap: 'wrap', gap: 'var(--space-xs)' }}>
          {chips.map(({ status, label }) => (
            <StatusChip key={status} status={status} label={label} />
          ))}
          <StatusChip status="ok" label={t('sample.keyValid')} />
          <StatusChip status="idle" label="EN → ES · EN" noDot />
        </Stack>
      </Section>

      <Section title={t('primitives.meter')}>
        <Gallery min="14rem">
          {meters.map(([label, db]) => (
            <State key={label} label={label}>
              <LevelMeter db={db} sx={{ width: '100%' }} />
            </State>
          ))}
        </Gallery>
        <Box sx={{ display: 'grid', gap: 'var(--space-xs)', maxWidth: '36rem' }}>
          <Box sx={{ display: 'flex', justifyContent: 'space-between', gap: 'var(--space-sm)' }}>
            <Typography variant="overline" sx={{ color: 'var(--color-muted)' }}>
              {t('primitives.meterDemo')}
            </Typography>
            <Mono>{Math.round(level)} dBFS · 16 kHz mono</Mono>
          </Box>
          <LevelMeter db={level} size="lg" />
          <Slider
            value={level}
            min={-60}
            max={0}
            step={0.5}
            onChange={(_, v) => setLevel(v as number)}
            aria-label={t('primitives.meterDemo')}
          />
        </Box>
      </Section>

      <Section title={t('primitives.panel')}>
        <Panel
          title={t('sample.card')}
          titleAs="h3"
          actions={
            <>
              <StatusChip status="live" />
              <Button
                variant="outlined"
                color="secondary"
                startIcon={<PauseOutlined aria-hidden />}
              >
                {t('sample.pause')}
              </Button>
              <Button variant="outlined" color="error" startIcon={<StopOutlined aria-hidden />}>
                {t('sample.stop')}
              </Button>
            </>
          }
        >
          <Box
            sx={{
              display: 'grid',
              gap: 'var(--space-md)',
              gridTemplateColumns: 'repeat(auto-fit, minmax(min(8rem, 100%), 1fr))',
            }}
          >
            <Stat
              label={t('sample.input')}
              value={<LevelMeter db={-18} />}
              sx={{ gridColumn: '1 / -1' }}
            />
            <Stat label={t('sample.provider')} value="Gemini" />
            <Stat label={t('sample.speaking')} value="EN" />
            <Stat label={t('sample.latency')} value="ES 2.4 s" detail="EN 1.3 s" />
            <Stat label={t('sample.viewers')} value="128" />
          </Box>
        </Panel>
        <Panel
          title={t('sample.roomB')}
          titleAs="h3"
          tone="error"
          actions={<StatusChip status="error" />}
        >
          <ErrorAlert
            error={{ code: 'provider.key_invalid', message: 'key invalid' }}
            action={
              <Button variant="outlined" color="secondary">
                {t('sample.retry')}
              </Button>
            }
          />
        </Panel>
      </Section>

      <Section title={t('primitives.copy')}>
        <Gallery min="24rem">
          <State label={t('state.default')}>
            <CopyField
              value={overlayUrl}
              label={t('sample.overlayUrl')}
              copyLabel={t('sample.copy')}
              sx={{ width: '100%' }}
            />
          </State>
          <State label={t('state.copied')}>
            <CopyField
              value={overlayUrl}
              label={t('sample.overlayUrl')}
              forceState="copied"
              sx={{ width: '100%' }}
            />
          </State>
          <State label={t('state.failed')}>
            <CopyField
              value={captureUrl}
              label={t('sample.captureUrl')}
              forceState="failed"
              sx={{ width: '100%' }}
            />
          </State>
          <State label={t('state.disabled')}>
            <CopyField
              value={captureUrl}
              label={t('sample.captureUrl')}
              disabled
              disabledReason={t('sample.reason')}
              sx={{ width: '100%' }}
            />
          </State>
        </Gallery>
      </Section>

      <Section title={t('primitives.kbd')}>
        <Stack
          direction="row"
          useFlexGap
          sx={{ flexWrap: 'wrap', gap: 'var(--space-md)', alignItems: 'center' }}
        >
          <Box
            sx={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 'var(--space-sm)',
              color: 'var(--color-muted)',
              fontSize: 'var(--text-sm)',
            }}
          >
            {t('sample.palette')} <KbdHint keys={['Mod', 'K']} />
          </Box>
          <Box sx={{ display: 'inline-flex', gap: 'var(--space-xs)', alignItems: 'center' }}>
            <KbdHint keys={['↑']} /> <KbdHint keys={['↓']} />
            <Typography variant="body2">{t('sample.moveHint')}</Typography>
          </Box>
          <Box sx={{ display: 'inline-flex', gap: 'var(--space-xs)', alignItems: 'center' }}>
            <KbdHint keys={['Esc']} />
            <Typography variant="body2">{t('sample.closeHint')}</Typography>
          </Box>
        </Stack>
      </Section>

      <Section title={t('primitives.qr')}>
        <Stack
          direction="row"
          useFlexGap
          sx={{ flexWrap: 'wrap', gap: 'var(--space-lg)', alignItems: 'center' }}
        >
          <QrCode value={viewerUrl} />
          <Box
            sx={{
              display: 'flex',
              gap: 'var(--space-sm)',
              alignItems: 'center',
              background: 'var(--stage-bg-dark)',
              color: 'var(--stage-fg-white)',
              padding: 'var(--space-sm)',
              borderRadius: 'var(--radius-card)',
              minWidth: 0,
            }}
          >
            <QrCode value={viewerUrl} size="5rem" />
            <Box sx={{ display: 'grid', gap: 'var(--space-2xs)', minWidth: 0 }}>
              <strong>{t('sample.qrHint')}</strong>
              <Box
                component="code"
                sx={{
                  fontFamily: 'var(--font-mono)',
                  fontSize: 'var(--text-sm)',
                  color: 'var(--stage-fg-secondary)',
                  overflowWrap: 'anywhere',
                }}
              >
                192.168.1.20:8080/s/main-stage
              </Box>
            </Box>
          </Box>
        </Stack>
      </Section>

      <Section title={t('primitives.languagePicker')}>
        <Gallery min="14rem">
          <LanguagePicker
            value={lang}
            onChange={setLang}
            languages={['es', 'en', 'pt']}
            includeSource
          />
          <LanguagePicker
            value="en"
            onChange={() => {}}
            languages={['es', 'en']}
            disabled
            helperText={t('sample.reason')}
          />
          <LanguagePicker
            value="es"
            onChange={() => {}}
            languages={['es', 'en']}
            error
            helperText={t('sample.trackError')}
          />
        </Gallery>
      </Section>

      <Section title={t('primitives.segmented')}>
        <Stack
          direction="row"
          useFlexGap
          sx={{ flexWrap: 'wrap', gap: 'var(--space-md)', alignItems: 'center' }}
        >
          <ThemeToggle />
          <UiLanguageSwitcher />
        </Stack>
        <Gallery min="16rem">
          <State label={t('state.selected')}>
            <ToggleButtonGroup
              exclusive
              value={seg}
              onChange={(_, v: string | null) => v && setSeg(v)}
              aria-label={t('primitives.segmented')}
              sx={segmentedSx}
            >
              {segOptions.map(({ v, name }) => (
                <ToggleButton key={v} value={v} lang={v}>
                  {name}
                </ToggleButton>
              ))}
            </ToggleButtonGroup>
          </State>
          <State label={t('state.focus')}>
            <ToggleButtonGroup exclusive value="es" aria-label={t('state.focus')} sx={segmentedSx}>
              {segOptions.map(({ v, name }) => (
                <ToggleButton key={v} value={v} className={v === 'en' ? 'Mui-focusVisible' : ''}>
                  {name}
                </ToggleButton>
              ))}
            </ToggleButtonGroup>
          </State>
          <State label={t('state.disabled')}>
            <ToggleButtonGroup
              exclusive
              value="es"
              disabled
              aria-label={t('state.disabled')}
              sx={segmentedSx}
            >
              {segOptions.map(({ v, name }) => (
                <ToggleButton key={v} value={v}>
                  {name}
                </ToggleButton>
              ))}
            </ToggleButtonGroup>
            <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
              {t('sample.reason')}
            </Typography>
          </State>
        </Gallery>
      </Section>

      <Section title={t('primitives.empty')}>
        <Panel>
          <EmptyState
            title={t('sample.emptyTitle')}
            description={t('sample.emptyBody')}
            titleAs="h3"
            action={
              <Button variant="contained" startIcon={<AddOutlined aria-hidden />}>
                {t('sample.newSession')}
              </Button>
            }
          />
        </Panel>
      </Section>

      <Section title={t('primitives.errors')}>
        <Gallery min="20rem">
          <State label="session.not_found">
            <ErrorAlert error={{ code: 'session.not_found', message: 'not found' }} />
          </State>
          <State label="network.unreachable">
            <ErrorAlert
              error={new TypeError('Failed to fetch')}
              action={
                <Button
                  variant="outlined"
                  color="secondary"
                  startIcon={<RefreshOutlined aria-hidden />}
                >
                  {t('sample.retry')}
                </Button>
              }
            />
          </State>
          <State label={t('state.unknown')}>
            <ErrorAlert error={{ code: 'whisper.timeout', message: 'timeout' }} />
          </State>
        </Gallery>
      </Section>
    </>
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

        <Primitives />

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
