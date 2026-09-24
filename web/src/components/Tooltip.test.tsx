// SPDX-License-Identifier: Apache-2.0
import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { Tooltip } from './Tooltip'

const hoverDelayMs = 800

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

function setup() {
  render(
    <Tooltip title="Copy overlay URL">
      <button>copy</button>
    </Tooltip>,
  )
  return screen.getByRole('button')
}

it('waits on hover', () => {
  const button = setup()
  fireEvent.mouseOver(button)
  act(() => vi.advanceTimersByTime(hoverDelayMs - 50))
  expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
  act(() => vi.advanceTimersByTime(100))
  expect(screen.getByRole('tooltip')).toHaveTextContent('Copy overlay URL')
})

it('does not open when the pointer leaves before the delay', () => {
  const button = setup()
  fireEvent.mouseOver(button)
  fireEvent.mouseLeave(button)
  act(() => vi.advanceTimersByTime(hoverDelayMs * 2))
  expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
})
