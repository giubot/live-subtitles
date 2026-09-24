// SPDX-License-Identifier: Apache-2.0
import { createFileRoute, redirect } from '@tanstack/react-router'

// The operator's entry point. Audiences arrive on /s/$id through the QR code.
export const Route = createFileRoute('/_themed/')({
  beforeLoad: () => {
    throw redirect({ to: '/admin' })
  },
})
