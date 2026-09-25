// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { GlossariesPage } from '../../../features/glossary/GlossariesPage'

export const Route = createFileRoute('/_themed/admin/glossaries')({
  component: GlossariesPage,
})
