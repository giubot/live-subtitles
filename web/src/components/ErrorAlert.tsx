// SPDX-License-Identifier: Apache-2.0
import ErrorOutlineOutlined from '@mui/icons-material/ErrorOutlineOutlined'
import Box from '@mui/material/Box'
import type { SxProps, Theme } from '@mui/material/styles'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { describeError } from './apiError'

export interface ErrorAlertProps {
  /** An API Error body (`{ code, params }`), a thrown fetch error, or anything else. */
  error: unknown
  /** The one thing to do next, e.g. a Retry button. */
  action?: ReactNode
  sx?: SxProps<Theme>
}

/**
 * Error message built from a translatable API code: what broke, why, and what
 * to do (docs/design.md § Copy). Unknown codes get a generic message plus
 * the code, so support can still find it in the server log.
 */
export function ErrorAlert({ error, action, sx }: ErrorAlertProps) {
  const { t, i18n } = useTranslation()
  const { title, why, fix, code, known } = describeError(i18n, error)
  return (
    <Box
      role="alert"
      sx={[
        {
          display: 'grid',
          gridTemplateColumns: 'auto minmax(0, 1fr)',
          columnGap: 'var(--space-xs)',
          rowGap: 'var(--space-2xs)',
          padding: 'var(--space-sm) var(--space-md)',
          border: 'var(--rule-hair) solid var(--color-danger)',
          borderRadius: 'var(--radius-input)',
          backgroundColor: 'var(--color-danger-soft)',
          color: 'var(--color-ink-2)',
          fontSize: 'var(--text-sm)',
          lineHeight: 'var(--leading-body)',
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
    >
      <ErrorOutlineOutlined
        aria-hidden
        sx={{ color: 'var(--color-danger)', fontSize: '1.25rem', marginBlockStart: '0.0625rem' }}
      />
      <Box
        component="strong"
        sx={{ color: 'var(--color-danger)', fontWeight: 'var(--weight-strong)' }}
      >
        {title}
      </Box>
      <Box sx={{ gridColumn: 2, maxInlineSize: 'var(--measure)' }}>
        {why} {fix}
      </Box>
      {!known && code && (
        <Box sx={{ gridColumn: 2, color: 'var(--color-neutral)' }}>
          {t('errors.codeLabel')}{' '}
          <Box component="code" sx={{ fontFamily: 'var(--font-mono)', overflowWrap: 'anywhere' }}>
            {code}
          </Box>
        </Box>
      )}
      {action != null && (
        <Box
          sx={{
            gridColumn: 2,
            display: 'flex',
            flexWrap: 'wrap',
            gap: 'var(--space-xs)',
            marginBlockStart: 'var(--space-2xs)',
          }}
        >
          {action}
        </Box>
      )}
    </Box>
  )
}
