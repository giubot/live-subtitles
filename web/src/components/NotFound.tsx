// SPDX-License-Identifier: Apache-2.0
import Button from '@mui/material/Button'
import Container from '@mui/material/Container'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { AppThemeProvider } from '../theme/AppThemeProvider'

export function NotFound() {
  const { t } = useTranslation()
  return (
    <AppThemeProvider>
      <Container component="main" maxWidth="sm" sx={{ paddingBlock: 'var(--space-xl)' }}>
        <Stack spacing={2} sx={{ alignItems: 'flex-start' }}>
          <Typography variant="h4" component="h1">
            {t('notFound.title')}
          </Typography>
          <Typography>{t('notFound.body')}</Typography>
          <Button variant="contained" component={Link} to="/">
            {t('notFound.action')}
          </Button>
        </Stack>
      </Container>
    </AppThemeProvider>
  )
}
