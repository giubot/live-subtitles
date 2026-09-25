// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { TlsPage } from '../../../features/settings/TlsPage'

export const Route = createFileRoute('/_themed/admin/tls')({
  component: TlsPage,
})
