// SPDX-License-Identifier: Apache-2.0
import HttpsOutlined from '@mui/icons-material/HttpsOutlined'
import KeyOutlined from '@mui/icons-material/KeyOutlined'
import LogoutOutlined from '@mui/icons-material/LogoutOutlined'
import MenuBookOutlined from '@mui/icons-material/MenuBookOutlined'
import SettingsOutlined from '@mui/icons-material/SettingsOutlined'
import SubtitlesOutlined from '@mui/icons-material/SubtitlesOutlined'
import ViewAgendaOutlined from '@mui/icons-material/ViewAgendaOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Typography from '@mui/material/Typography'
import { useQueryClient } from '@tanstack/react-query'
import { Link, Navigate, Outlet } from '@tanstack/react-router'
import { useEffect, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { ErrorAlert } from '../../components/ErrorAlert'
import { ThemeToggle } from '../../components/ThemeToggle'
import { UiLanguageSwitcher } from '../../components/UiLanguageSwitcher'
import { useAdminEvents } from '../../realtime/admin'
import { LoginPage } from './LoginPage'

/**
 * `/admin`: signs the operator in (or sends a fresh install to /setup),
 * then frames every admin page with the side rail (N3) and follows
 * /ws/admin for live session statuses (ADM-3).
 */
export function AdminLayout() {
  const me = api.useQuery('get', '/api/auth/me')
  const signedOut = me.data?.authenticated === false
  const setup = api.useQuery('get', '/api/setup', undefined, { enabled: signedOut })
  // These operations list no error responses, but the network can still fail.
  const meError: unknown = me.error
  const setupError: unknown = setup.error

  if (meError != null)
    return (
      <Centered>
        <ErrorAlert error={meError} />
      </Centered>
    )
  if (!me.data) return null
  if (signedOut) {
    if (setupError != null)
      return (
        <Centered>
          <ErrorAlert error={setupError} />
        </Centered>
      )
    if (!setup.data) return null
    if (!setup.data.adminPinSet) return <Navigate to="/setup" replace />
    return <LoginPage />
  }
  return <Shell />
}

function Centered({ children }: { children: ReactNode }) {
  return (
    <Box
      component="main"
      sx={{ maxInlineSize: '28rem', marginInline: 'auto', padding: 'var(--space-lg)' }}
    >
      {children}
    </Box>
  )
}

export function Wordmark() {
  const { t } = useTranslation()
  return (
    <Box
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--space-xs)',
        fontFamily: 'var(--font-display)',
        fontWeight: 700,
        fontSize: 'var(--text-md)',
        lineHeight: 1,
        letterSpacing: '-0.03em',
        color: 'var(--color-ink)',
        whiteSpace: 'nowrap',
      }}
    >
      {t('appName')}
      <Box
        component="span"
        aria-hidden
        sx={{
          fontFamily: 'var(--font-mono)',
          fontWeight: 500,
          fontSize: 'var(--text-xs)',
          letterSpacing: 'var(--tracking-label)',
          color: 'var(--color-accent-ink)',
          backgroundColor: 'var(--color-accent)',
          paddingBlock: 'var(--space-3xs)',
          paddingInline: 'var(--space-2xs)',
          borderRadius: 'var(--radius-chip)',
        }}
      >
        CC
      </Box>
    </Box>
  )
}

const wide = '@media (min-width: 60rem)'

function Shell() {
  const { t } = useTranslation('admin')
  useAdminEvents()
  const network = api.useQuery('get', '/api/network')
  const host = network.data
    ? `${network.data.preferredIp ?? 'localhost'}:${network.data.httpPort}`
    : undefined

  return (
    <Box
      sx={{
        minBlockSize: '100dvh',
        display: 'grid',
        gridTemplateColumns: 'minmax(0, 1fr)',
        [wide]: { gridTemplateColumns: 'var(--rail-width) minmax(0, 1fr)' },
      }}
    >
      <Box
        component="nav"
        aria-label={t('nav.label')}
        sx={{
          display: 'none',
          [wide]: { display: 'grid' },
          alignContent: 'start',
          gap: 'var(--space-2xs)',
          padding: 'var(--space-md) var(--space-sm)',
          backgroundColor: 'var(--color-paper-2)',
          borderInlineEnd: 'var(--rule-hair) solid var(--color-rule)',
        }}
      >
        <Box sx={{ padding: 'var(--space-xs) var(--space-xs) var(--space-md)' }}>
          <Wordmark />
        </Box>
        <RailLink to="/admin" icon={<ViewAgendaOutlined />}>
          {t('nav.sessions')}
        </RailLink>
        <RailLink to="/admin/glossaries" icon={<MenuBookOutlined />}>
          {t('nav.glossaries')}
        </RailLink>
        <RailLink to="/admin/overlays" icon={<SubtitlesOutlined />}>
          {t('nav.overlays')}
        </RailLink>
        <RailLink to="/admin/providers" icon={<KeyOutlined />}>
          {t('nav.providers')}
        </RailLink>
        <RailLink to="/admin/settings" icon={<SettingsOutlined />}>
          {t('nav.settings')}
        </RailLink>
        <RailLink to="/admin/tls" icon={<HttpsOutlined />}>
          {t('nav.tls')}
        </RailLink>
        {host && (
          <Box
            sx={{
              display: 'grid',
              gap: 'var(--space-2xs)',
              marginBlockStart: 'var(--space-lg)',
              padding: 'var(--space-sm)',
              borderBlockStart: 'var(--rule-hair) solid var(--color-rule)',
            }}
          >
            <Typography variant="overline" component="span">
              {t('nav.server')}
            </Typography>
            <Box
              component="span"
              sx={{
                fontFamily: 'var(--font-mono)',
                fontSize: 'var(--text-sm)',
                color: 'var(--color-ink)',
              }}
            >
              {host}
            </Box>
          </Box>
        )}
      </Box>
      <Box sx={{ minInlineSize: 0 }}>
        <Outlet />
      </Box>
    </Box>
  )
}

type AdminPath =
  | '/admin'
  | '/admin/glossaries'
  | '/admin/overlays'
  | '/admin/providers'
  | '/admin/settings'
  | '/admin/tls'

function RailLink({ to, icon, children }: { to: AdminPath; icon: ReactNode; children: ReactNode }) {
  return (
    <Box
      component={Link}
      to={to}
      activeOptions={{ exact: true }}
      sx={{
        position: 'relative',
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--space-sm)',
        minBlockSize: '2.5rem',
        paddingInline: 'var(--space-sm)',
        borderRadius: 'var(--radius-input)',
        color: 'var(--color-ink-2)',
        textDecoration: 'none',
        whiteSpace: 'nowrap',
        transition: 'background-color var(--dur-micro) var(--ease-out)',
        '& svg': { fontSize: '1.1rem', color: 'var(--color-muted)' },
        '@media (hover: hover)': { '&:hover': { backgroundColor: 'var(--color-paper-3)' } },
        '&[aria-current="page"]': {
          backgroundColor: 'var(--color-accent-soft)',
          color: 'var(--color-accent-text)',
          fontWeight: 700,
          '& svg': { color: 'var(--color-accent-text)' },
          '&::before': {
            content: '""',
            position: 'absolute',
            insetInlineStart: 'calc(-1 * var(--space-sm))',
            insetBlock: '25%',
            inlineSize: 'var(--space-2xs)',
            borderRadius: '0 var(--space-3xs) var(--space-3xs) 0',
            backgroundColor: 'var(--color-accent)',
          },
        },
      }}
    >
      <Box component="span" aria-hidden sx={{ display: 'flex' }}>
        {icon}
      </Box>
      {children}
    </Box>
  )
}

export interface AdminPageProps {
  title: string
  /** The page's main action (at most one filled button per view). */
  actions?: ReactNode
  children: ReactNode
}

/** An admin page: flat top bar with the title and actions, then content. */
export function AdminPage({ title, actions, children }: AdminPageProps) {
  const { t } = useTranslation('admin')
  const queryClient = useQueryClient()
  const logout = api.useMutation('post', '/api/auth/logout', {
    onSettled: () => queryClient.invalidateQueries(),
  })
  useEffect(() => {
    document.title = `${title} · ${t('appTitle')}`
  }, [t, title])

  return (
    <>
      <Box
        component="header"
        sx={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          gap: 'var(--space-sm)',
          paddingBlock: 'var(--space-sm)',
          paddingInline: 'var(--space-md)',
          borderBlockEnd: 'var(--rule-hair) solid var(--color-rule)',
        }}
      >
        <Box sx={{ display: 'contents', [wide]: { display: 'none' } }}>
          <Wordmark />
        </Box>
        <Typography variant="h1" sx={{ fontSize: 'var(--text-lg)', marginInlineEnd: 'auto' }}>
          {title}
        </Typography>
        {actions}
        <UiLanguageSwitcher />
        <ThemeToggle />
        <Button
          variant="text"
          color="secondary"
          startIcon={<LogoutOutlined aria-hidden />}
          onClick={() => logout.mutate({})}
        >
          {t('logout')}
        </Button>
      </Box>
      <Box sx={{ padding: 'var(--space-md)' }}>{children}</Box>
    </>
  )
}
