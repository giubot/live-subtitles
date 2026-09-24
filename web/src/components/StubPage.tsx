// SPDX-License-Identifier: Apache-2.0
import Container from '@mui/material/Container'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useTranslation } from 'react-i18next'
import { ThemeToggle } from './ThemeToggle'
import { UiLanguageSwitcher } from './UiLanguageSwitcher'

/** Placeholder for routes whose feature task hasn't landed yet. */
export function StubPage({ title }: { title: string }) {
  const { t } = useTranslation()
  return (
    <Container component="main" maxWidth="md" sx={{ py: 4 }}>
      <Stack spacing={2} sx={{ alignItems: 'flex-start' }}>
        <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap' }}>
          <UiLanguageSwitcher />
          <ThemeToggle />
        </Stack>
        <Typography variant="h4" component="h1">
          {title}
        </Typography>
        <Typography>{t('stub')}</Typography>
      </Stack>
    </Container>
  )
}
