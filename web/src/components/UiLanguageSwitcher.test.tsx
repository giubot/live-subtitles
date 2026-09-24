// SPDX-License-Identifier: Apache-2.0
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, expect, it } from 'vitest'
import i18n from '../i18n'
import { UiLanguageSwitcher } from './UiLanguageSwitcher'

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

it('switches the UI language and remembers it', async () => {
  render(<UiLanguageSwitcher />)
  const group = screen.getByRole('group', { name: 'Interface language' })
  expect(group).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'English' })).toHaveAttribute('aria-pressed', 'true')

  await userEvent.click(screen.getByRole('button', { name: 'Español' }))

  expect(i18n.resolvedLanguage).toBe('es')
  expect(localStorage.getItem('ls.ui')).toBe('es')
  expect(document.documentElement.lang).toBe('es')
  expect(screen.getByRole('group', { name: 'Idioma de la interfaz' })).toBeInTheDocument()
})
