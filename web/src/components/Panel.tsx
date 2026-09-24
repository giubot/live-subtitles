// SPDX-License-Identifier: Apache-2.0
import Box from '@mui/material/Box'
import type { SxProps, Theme } from '@mui/material/styles'
import Typography from '@mui/material/Typography'
import { useId, type ReactNode } from 'react'

export interface PanelProps {
  /** Panel heading, labels the region when given. */
  title?: ReactNode
  /** Heading level for the title; pick the one that fits the page outline. */
  titleAs?: 'h2' | 'h3' | 'h4'
  /** Controls at the end of the header row (buttons, a status chip…). */
  actions?: ReactNode
  /** `error` draws the border in danger, e.g. a session that failed. */
  tone?: 'default' | 'error'
  children?: ReactNode
  sx?: SxProps<Theme>
}

/**
 * Hairline container: rule-2 border, 10 px radius, no shadow
 * (docs/design.md § Space, shape, depth). Don't nest panels.
 */
export function Panel({ title, titleAs = 'h2', actions, tone, children, sx }: PanelProps) {
  const titleId = useId()
  const hasHeader = title != null || actions != null
  return (
    <Box
      component={title != null ? 'section' : 'div'}
      aria-labelledby={title != null ? titleId : undefined}
      sx={[
        {
          border: 'var(--rule-hair) solid',
          borderColor: tone === 'error' ? 'var(--color-danger)' : 'var(--color-rule-2)',
          borderRadius: 'var(--radius-card)',
          backgroundColor: 'var(--color-paper)',
          padding: 'var(--space-md)',
          display: 'grid',
          gap: 'var(--space-md)',
          minInlineSize: 0,
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
    >
      {hasHeader && (
        <Box
          sx={{
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            gap: 'var(--space-xs) var(--space-sm)',
          }}
        >
          {title != null && (
            <Typography
              id={titleId}
              variant="h5"
              component={titleAs}
              sx={{ marginInlineEnd: 'auto', minInlineSize: 0, overflowWrap: 'anywhere' }}
            >
              {title}
            </Typography>
          )}
          {actions != null && (
            <Box
              sx={{
                display: 'flex',
                flexWrap: 'wrap',
                alignItems: 'center',
                gap: 'var(--space-xs)',
                marginInlineStart: 'auto',
              }}
            >
              {actions}
            </Box>
          )}
        </Box>
      )}
      {children}
    </Box>
  )
}
