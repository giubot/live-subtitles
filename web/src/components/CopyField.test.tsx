// SPDX-License-Identifier: Apache-2.0
import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '../i18n'
import { CopyField } from './CopyField'

const url = 'http://192.168.1.20:8080/overlay/main-stage?lang=es'

function setClipboard(value: unknown) {
  Object.defineProperty(navigator, 'clipboard', { value, configurable: true })
}
function setExecCommand(value: unknown) {
  Object.defineProperty(document, 'execCommand', { value, configurable: true, writable: true })
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
})
afterEach(() => {
  setClipboard(undefined)
  setExecCommand(undefined)
})

async function clickCopy(name = 'Copy') {
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name }))
  })
}

describe('CopyField', () => {
  it('shows the value and describes the button with it', () => {
    render(<CopyField value={url} label="Overlay URL" copyLabel="Copy overlay URL" />)
    expect(screen.getByText(url)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy overlay URL' })).toHaveAccessibleDescription(
      url,
    )
  })

  it('copies with the Clipboard API and announces it politely', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    setClipboard({ writeText })
    const onCopied = vi.fn()
    render(<CopyField value={url} onCopied={onCopied} />)
    expect(screen.getByRole('status')).toHaveTextContent('')

    await clickCopy()

    expect(writeText).toHaveBeenCalledWith(url)
    expect(onCopied).toHaveBeenCalledOnce()
    expect(screen.getByRole('status')).toHaveTextContent('Copied')
    expect(screen.getByRole('button', { name: 'Copied' })).toBeInTheDocument()
  })

  it('goes back to "Copy" after a moment', async () => {
    vi.useFakeTimers()
    try {
      setClipboard({ writeText: vi.fn().mockResolvedValue(undefined) })
      render(<CopyField value={url} />)
      await clickCopy()
      expect(screen.getByRole('button', { name: 'Copied' })).toBeInTheDocument()
      act(() => vi.advanceTimersByTime(2500))
      expect(screen.getByRole('button', { name: 'Copy' })).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('falls back to execCommand when there is no Clipboard API (plain http)', async () => {
    setClipboard(undefined)
    let copied = ''
    const exec = vi.fn(() => {
      copied = document.querySelector('textarea')?.value ?? ''
      return true
    })
    setExecCommand(exec)
    render(<CopyField value={url} />)

    await clickCopy()

    expect(exec).toHaveBeenCalledWith('copy')
    expect(copied).toBe(url)
    expect(document.querySelector('textarea')).toBeNull() // cleaned up
    expect(screen.getByRole('status')).toHaveTextContent('Copied')
  })

  it('falls back when the Clipboard API rejects', async () => {
    setClipboard({ writeText: vi.fn().mockRejectedValue(new Error('denied')) })
    const exec = vi.fn(() => true)
    setExecCommand(exec)
    render(<CopyField value={url} />)
    await clickCopy()
    expect(exec).toHaveBeenCalledWith('copy')
    expect(screen.getByRole('status')).toHaveTextContent('Copied')
  })

  it('selects the text and says what to do when copying is blocked', async () => {
    setClipboard(undefined)
    setExecCommand(vi.fn(() => false))
    render(<CopyField value={url} />)

    await clickCopy()

    expect(screen.getByRole('status')).toHaveTextContent('The browser blocked clipboard access')
    expect(screen.getByRole('button', { name: 'Copy again' })).toBeInTheDocument()
    expect(window.getSelection()?.toString()).toBe(url)
  })

  it('keeps the reason next to a disabled button', () => {
    render(<CopyField value={url} disabled disabledReason="Available once the session is live" />)
    const button = screen.getByRole('button', { name: 'Copy' })
    expect(button).toBeDisabled()
    expect(button).toHaveAccessibleDescription(`${url} Available once the session is live`)
  })
})
