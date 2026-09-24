// SPDX-License-Identifier: Apache-2.0
import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import i18n from '../i18n'
import { describeError, toApiError } from './apiError'
import { ErrorAlert } from './ErrorAlert'

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('toApiError', () => {
  it.each([
    [{ code: 'session.not_found', message: 'no such session' }, 'session.not_found'],
    [new TypeError('Failed to fetch'), 'network.unreachable'],
    [new TypeError('Load failed'), 'network.unreachable'],
    [new TypeError('x is undefined'), undefined],
    [new Error('boom'), undefined],
    [{ code: 42 }, undefined],
    [null, undefined],
    ['text', undefined],
  ])('%s → %s', (input, code) => {
    expect(toApiError(input)?.code).toBe(code)
  })
})

describe('describeError', () => {
  it('translates a known code into what broke, why and what to do', () => {
    const d = describeError(i18n, { code: 'session.not_found', message: 'x' })
    expect(d).toMatchObject({
      known: true,
      code: 'session.not_found',
      title: 'Session not found',
      why: 'It may have been deleted, or the link has a typo.',
      fix: 'Check the link, or pick the session from the list.',
    })
  })

  it('fills placeholders from params', () => {
    i18n.addResource('en', 'common', 'errors.test.port_busy.title', 'Port {{port}} is busy')
    i18n.addResource('en', 'common', 'errors.test.port_busy.why', 'Something else uses it.')
    i18n.addResource('en', 'common', 'errors.test.port_busy.fix', 'Stop it.')
    const d = describeError(i18n, { code: 'test.port_busy', params: { port: 8080 } })
    expect(d.title).toBe('Port 8080 is busy')
  })

  it('falls back to the generic message and keeps the code', () => {
    const d = describeError(i18n, { code: 'whisper.timeout', message: 'timeout' })
    expect(d).toMatchObject({ known: false, code: 'whisper.timeout', title: 'The request failed' })
  })

  it('does not treat odd codes as translation keys', () => {
    expect(describeError(i18n, { code: '../errors' }).known).toBe(false)
    expect(describeError(i18n, { code: 'generic' }).title).toBe('The request failed')
  })
})

describe('ErrorAlert', () => {
  it('renders a known code as an alert with its action', () => {
    render(
      <ErrorAlert
        error={{ code: 'provider.key_invalid', message: 'bad key' }}
        action={<button>Retry</button>}
      />,
    )
    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent('Google rejected the API key')
    expect(alert).toHaveTextContent('Check it in Google AI Studio')
    expect(alert).not.toHaveTextContent('provider.key_invalid')
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
  })

  it('shows the generic message plus the code for an unknown code', () => {
    render(<ErrorAlert error={{ code: 'whisper.timeout', message: 'timeout' }} />)
    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent('The request failed')
    expect(alert).toHaveTextContent('Error code: whisper.timeout')
  })

  it('follows the UI language', async () => {
    await i18n.changeLanguage('es')
    render(<ErrorAlert error={new TypeError('Failed to fetch')} />)
    expect(screen.getByRole('alert')).toHaveTextContent('No se puede conectar con el servidor')
  })
})
