// SPDX-License-Identifier: Apache-2.0
import InboxOutlined from '@mui/icons-material/InboxOutlined'
import Box from '@mui/material/Box'
import type { SxProps, Theme } from '@mui/material/styles'
import Typography from '@mui/material/Typography'
import type { ReactNode } from 'react'

export interface EmptyStateProps {
  /** What's empty ("No sessions yet"). */
  title: ReactNode
  /** Why it's empty, or what will appear here. */
  description: ReactNode
  /** The one action that fills it, e.g. a "New session" button. */
  action?: ReactNode
  /** An Outlined icon; defaults to an inbox. Rendered aria-hidden. */
  icon?: ReactNode
  /** Heading level for the title. */
  titleAs?: 'h2' | 'h3' | 'h4'
  sx?: SxProps<Theme>
}

/** Placeholder for an empty list or view: what's empty, why, and one action. */
export function EmptyState({
  title,
  description,
  action,
  icon,
  titleAs = 'h2',
  sx,
}: EmptyStateProps) {
  return (
    <Box
      sx={[
        {
          display: 'grid',
          justifyItems: 'center',
          textAlign: 'center',
          gap: 'var(--space-sm)',
          paddingBlock: 'var(--space-xl)',
          paddingInline: 'var(--space-md)',
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
    >
      <Box
        aria-hidden
        sx={{
          display: 'grid',
          placeItems: 'center',
          inlineSize: 'var(--space-xl)',
          blockSize: 'var(--space-xl)',
          borderRadius: 'var(--radius-card)',
          backgroundColor: 'var(--color-paper-2)',
          border: 'var(--rule-hair) solid var(--color-rule)',
          color: 'var(--color-muted)',
          '& svg': { fontSize: '1.25rem' },
        }}
      >
        {icon ?? <InboxOutlined />}
      </Box>
      <Typography variant="h5" component={titleAs}>
        {title}
      </Typography>
      <Typography sx={{ color: 'var(--color-neutral)', maxInlineSize: 'var(--measure)' }}>
        {description}
      </Typography>
      {action != null && <Box sx={{ marginBlockStart: 'var(--space-2xs)' }}>{action}</Box>}
    </Box>
  )
}
