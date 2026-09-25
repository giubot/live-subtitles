// SPDX-License-Identifier: Apache-2.0
import AddOutlined from '@mui/icons-material/AddOutlined'
import CloseOutlined from '@mui/icons-material/CloseOutlined'
import ContentPasteOutlined from '@mui/icons-material/ContentPasteOutlined'
import DeleteOutlined from '@mui/icons-material/DeleteOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import IconButton from '@mui/material/IconButton'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useQueryClient } from '@tanstack/react-query'
import { useId, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { useLanguages } from '../../api/languages'
import { describeError, toApiError } from '../../components/apiError'
import { ErrorAlert } from '../../components/ErrorAlert'
import { nativeLanguageName } from '../../components/languageNames'
import {
  emptyRow,
  importRows,
  languagePattern,
  sentRowKeys,
  toBody,
  valuesFrom,
  type Glossary,
  type GlossaryFormValues,
  type TermRow,
} from './glossaryForm'

export interface GlossaryDialogProps {
  /** The glossary to edit; absent to create one. */
  glossary?: Glossary
  onClose: () => void
}

const listKey = ['get', '/api/glossaries'] as const

/**
 * Create or edit a glossary (AI-7): terms with their preferred
 * translation per language, terms to keep as they are, and a CSV/TSV
 * paste import for lists kept in a spreadsheet.
 */
export function GlossaryDialog({ glossary, onClose }: GlossaryDialogProps) {
  const { t, i18n } = useTranslation('glossary')
  const queryClient = useQueryClient()
  const creating = !glossary
  const [v, setV] = useState<GlossaryFormValues>(() => valuesFrom(glossary))
  const [touched, setTouched] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [pasteOpen, setPasteOpen] = useState(false)
  const [paste, setPaste] = useState('')
  const [imported, setImported] = useState<{ added: number; updated: number }>()
  const [newLang, setNewLang] = useState('')
  /** Row keys in the order the last save sent them, to place the server's `terms.N.*` errors. */
  const [sentKeys, setSentKeys] = useState<number[]>([])
  const catalog = useLanguages()
  const suggestionsId = useId()
  const refresh = () => queryClient.invalidateQueries({ queryKey: listKey })

  const create = api.useMutation('post', '/api/glossaries', {
    onSuccess: () => {
      void refresh()
      onClose()
    },
  })
  const update = api.useMutation('put', '/api/glossaries/{glossaryId}', {
    onSuccess: () => {
      void refresh()
      onClose()
    },
  })
  const remove = api.useMutation('delete', '/api/glossaries/{glossaryId}', {
    onSuccess: () => {
      void refresh()
      // The server detaches it from the sessions and the settings default that named it.
      void queryClient.invalidateQueries({ queryKey: ['get', '/api/sessions'] })
      void queryClient.invalidateQueries({ queryKey: ['get', '/api/settings'] })
      onClose()
    },
  })
  const error: unknown = create.error ?? update.error ?? remove.error
  const pending = create.isPending || update.isPending || remove.isPending
  const serverFields = toApiError(error)?.fields ?? {}
  const nameInvalid = (touched && v.name.trim() === '') || !!serverFields.name
  const keepInvalid = Object.keys(serverFields).some((k) => k.startsWith('doNotTranslate'))
  /** The server's error for a row's cell (`term`, `note`, `translations.es`), as a short text. */
  const cellError = (row: TermRow, part: string): string | undefined => {
    const i = sentKeys.indexOf(row.key)
    const code = i < 0 ? undefined : serverFields[`terms.${i}.${part}`]
    return code ? describeError(i18n, { code }).title : undefined
  }

  const setRow = (key: number, patch: Partial<TermRow>) =>
    setV((prev) => ({
      ...prev,
      rows: prev.rows.map((r) => (r.key === key ? { ...r, ...patch } : r)),
    }))
  const setTranslation = (row: TermRow, lang: string, text: string) =>
    setRow(row.key, { translations: { ...row.translations, [lang]: text } })

  const addLanguage = () => {
    const lang = newLang.trim()
    if (!languagePattern.test(lang)) return
    if (!v.languages.includes(lang))
      setV((prev) => ({ ...prev, languages: [...prev.languages, lang] }))
    setNewLang('')
  }

  const runImport = () => {
    const result = importRows(v, paste)
    setV(result.values)
    setImported({ added: result.added, updated: result.updated })
    setPaste('')
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    setTouched(true)
    if (v.name.trim() === '') return
    const body = toBody(v)
    setSentKeys(sentRowKeys(v))
    if (creating) create.mutate({ body })
    else update.mutate({ params: { path: { glossaryId: glossary.id } }, body })
  }

  const cell = { paddingBlock: 'var(--space-2xs)', paddingInline: 'var(--space-2xs)' }
  const small = { htmlInput: { sx: { paddingBlock: 'var(--space-xs)' } } }

  return (
    <Dialog
      open
      onClose={pending ? undefined : onClose}
      fullWidth
      maxWidth="lg"
      aria-labelledby="glossary-dialog-title"
    >
      <Box component="form" onSubmit={submit} noValidate>
        <DialogTitle id="glossary-dialog-title">
          {creating ? t('form.createTitle') : t('form.editTitle', { name: glossary.name })}
        </DialogTitle>
        <DialogContent sx={{ display: 'grid', gap: 'var(--space-md)' }}>
          {error != null && <ErrorAlert error={error} />}
          <TextField
            label={t('form.name')}
            value={v.name}
            autoFocus
            onChange={(e) => setV((prev) => ({ ...prev, name: e.target.value }))}
            error={nameInvalid}
            helperText={nameInvalid ? t('form.nameInvalid') : t('form.nameHelp')}
            slotProps={{
              inputLabel: { shrink: true },
              htmlInput: { maxLength: 120, 'aria-invalid': nameInvalid || undefined },
            }}
            sx={{ maxInlineSize: '32rem' }}
          />

          <Box sx={{ display: 'grid', gap: 'var(--space-xs)' }}>
            <Box
              sx={{
                display: 'flex',
                flexWrap: 'wrap',
                alignItems: 'center',
                gap: 'var(--space-xs) var(--space-sm)',
              }}
            >
              <Typography variant="h5" component="h3" sx={{ marginInlineEnd: 'auto' }}>
                {t('terms.heading', { count: v.rows.filter((r) => r.term.trim()).length })}
              </Typography>
              <TextField
                label={t('terms.addLanguage')}
                value={newLang}
                onChange={(e) => setNewLang(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    addLanguage()
                  }
                }}
                size="small"
                slotProps={{
                  inputLabel: { shrink: true },
                  htmlInput: {
                    list: suggestionsId,
                    maxLength: 12,
                    spellCheck: false,
                    sx: { fontFamily: 'var(--font-mono)' },
                  },
                }}
                sx={{ inlineSize: '9rem' }}
              />
              <datalist id={suggestionsId}>
                {catalog.targets
                  .filter((l) => !v.languages.includes(l))
                  .map((l) => (
                    <option key={l} value={l}>
                      {catalog.byCode.get(l)?.nativeName ?? nativeLanguageName(l)}
                    </option>
                  ))}
              </datalist>
              <Button
                variant="outlined"
                color="secondary"
                onClick={addLanguage}
                disabled={!languagePattern.test(newLang.trim())}
              >
                {t('terms.addLanguageAction')}
              </Button>
              <Button
                variant="outlined"
                color="secondary"
                startIcon={<ContentPasteOutlined aria-hidden />}
                aria-expanded={pasteOpen}
                onClick={() => setPasteOpen((o) => !o)}
              >
                {t('import.open')}
              </Button>
            </Box>

            {pasteOpen && (
              <Box
                sx={{
                  display: 'grid',
                  gap: 'var(--space-xs)',
                  padding: 'var(--space-sm)',
                  borderRadius: 'var(--radius-input)',
                  backgroundColor: 'var(--color-paper-2)',
                  border: 'var(--rule-hair) solid var(--color-rule)',
                }}
              >
                <TextField
                  label={t('import.label')}
                  multiline
                  minRows={4}
                  maxRows={12}
                  value={paste}
                  onChange={(e) => setPaste(e.target.value)}
                  helperText={t('import.help', {
                    columns: ['term', ...v.languages, 'note'].join(', '),
                  })}
                  slotProps={{
                    inputLabel: { shrink: true },
                    htmlInput: { spellCheck: false, sx: { fontFamily: 'var(--font-mono)' } },
                  }}
                />
                <Box
                  sx={{
                    display: 'flex',
                    flexWrap: 'wrap',
                    alignItems: 'center',
                    gap: 'var(--space-sm)',
                  }}
                >
                  <Button
                    variant="outlined"
                    color="secondary"
                    onClick={runImport}
                    disabled={paste.trim() === ''}
                  >
                    {t('import.action')}
                  </Button>
                  {imported && (
                    <Typography
                      variant="body2"
                      role="status"
                      sx={{ color: 'var(--color-neutral)' }}
                    >
                      {t('import.result', imported)}
                    </Typography>
                  )}
                </Box>
              </Box>
            )}

            <Box sx={{ overflowX: 'auto', minInlineSize: 0 }}>
              <Box
                component="table"
                sx={{
                  inlineSize: '100%',
                  borderCollapse: 'collapse',
                  '& th': {
                    ...cell,
                    textAlign: 'start',
                    fontFamily: 'var(--font-mono)',
                    fontSize: 'var(--text-xs)',
                    fontWeight: 500,
                    letterSpacing: 'var(--tracking-label)',
                    textTransform: 'uppercase',
                    color: 'var(--color-neutral)',
                    whiteSpace: 'nowrap',
                    borderBlockEnd: 'var(--rule-hair) solid var(--color-rule)',
                  },
                  '& td': { ...cell, verticalAlign: 'middle' },
                  '& td .MuiTextField-root': { minInlineSize: '9rem', inlineSize: '100%' },
                }}
              >
                <thead>
                  <tr>
                    <th scope="col">{t('terms.term')}</th>
                    {v.languages.map((lang) => (
                      <th key={lang} scope="col" lang={lang}>
                        {nativeLanguageName(lang)} · {lang}
                      </th>
                    ))}
                    <th scope="col">{t('terms.note')}</th>
                    <th scope="col">{t('terms.keep')}</th>
                    <th scope="col">
                      <Box component="span" sx={visuallyHidden}>
                        {t('terms.actions')}
                      </Box>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {v.rows.map((row, i) => {
                    const label = row.term.trim() || t('terms.rowN', { n: i + 1 })
                    return (
                      <tr key={row.key}>
                        <td>
                          <TextField
                            value={row.term}
                            onChange={(e) => setRow(row.key, { term: e.target.value })}
                            error={!!cellError(row, 'term')}
                            helperText={cellError(row, 'term')}
                            slotProps={{
                              ...small,
                              htmlInput: {
                                ...small.htmlInput,
                                'aria-label': t('terms.termOf', { n: i + 1 }),
                                'aria-invalid': !!cellError(row, 'term') || undefined,
                              },
                            }}
                          />
                        </td>
                        {v.languages.map((lang) => (
                          <td key={lang}>
                            <TextField
                              value={row.translations[lang] ?? ''}
                              disabled={row.keep}
                              onChange={(e) => setTranslation(row, lang, e.target.value)}
                              error={!!cellError(row, `translations.${lang}`)}
                              helperText={cellError(row, `translations.${lang}`)}
                              slotProps={{
                                htmlInput: {
                                  ...small.htmlInput,
                                  lang,
                                  'aria-label': t('terms.translationOf', {
                                    term: label,
                                    lang: nativeLanguageName(lang),
                                  }),
                                },
                              }}
                            />
                          </td>
                        ))}
                        <td>
                          <TextField
                            value={row.note}
                            onChange={(e) => setRow(row.key, { note: e.target.value })}
                            error={!!cellError(row, 'note')}
                            helperText={cellError(row, 'note')}
                            slotProps={{
                              htmlInput: {
                                ...small.htmlInput,
                                'aria-label': t('terms.noteOf', { term: label }),
                              },
                            }}
                          />
                        </td>
                        <td>
                          <Checkbox
                            checked={row.keep}
                            onChange={(e) => setRow(row.key, { keep: e.target.checked })}
                            slotProps={{
                              input: { 'aria-label': t('terms.keepOf', { term: label }) },
                            }}
                          />
                        </td>
                        <td>
                          <IconButton
                            aria-label={t('terms.remove', { term: label })}
                            onClick={() =>
                              setV((prev) => ({
                                ...prev,
                                rows: prev.rows.filter((r) => r.key !== row.key),
                              }))
                            }
                          >
                            <CloseOutlined fontSize="small" />
                          </IconButton>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </Box>
            </Box>
            <Box>
              <Button
                variant="text"
                startIcon={<AddOutlined aria-hidden />}
                onClick={() => setV((prev) => ({ ...prev, rows: [...prev.rows, emptyRow()] }))}
              >
                {t('terms.add')}
              </Button>
            </Box>
            <Typography
              variant="body2"
              sx={{ color: 'var(--color-neutral)', maxInlineSize: 'var(--measure)' }}
            >
              {t('terms.keepHelp')}
            </Typography>
          </Box>

          <TextField
            label={t('keep.label')}
            multiline
            minRows={2}
            maxRows={8}
            value={v.extraKeep}
            onChange={(e) => setV((prev) => ({ ...prev, extraKeep: e.target.value }))}
            error={keepInvalid}
            helperText={t('keep.help')}
            slotProps={{ inputLabel: { shrink: true }, htmlInput: { spellCheck: false } }}
            sx={{ maxInlineSize: '32rem' }}
          />
        </DialogContent>
        <DialogActions
          sx={{ flexWrap: 'wrap', gap: 'var(--space-xs)', padding: 'var(--space-md)' }}
        >
          {!creating &&
            (confirmDelete ? (
              <Box
                sx={{
                  display: 'flex',
                  flexWrap: 'wrap',
                  alignItems: 'center',
                  gap: 'var(--space-xs)',
                  marginInlineEnd: 'auto',
                }}
              >
                <Box
                  component="span"
                  sx={{ fontSize: 'var(--text-sm)', color: 'var(--color-danger)' }}
                >
                  {t('form.deleteConfirm')}
                </Box>
                <Button
                  variant="outlined"
                  color="error"
                  loading={remove.isPending}
                  onClick={() => remove.mutate({ params: { path: { glossaryId: glossary.id } } })}
                >
                  {t('form.deleteYes')}
                </Button>
                <Button variant="text" color="secondary" onClick={() => setConfirmDelete(false)}>
                  {t('form.deleteNo')}
                </Button>
              </Box>
            ) : (
              <Button
                variant="outlined"
                color="error"
                startIcon={<DeleteOutlined aria-hidden />}
                onClick={() => setConfirmDelete(true)}
                sx={{ marginInlineEnd: 'auto' }}
              >
                {t('form.delete')}
              </Button>
            ))}
          <Button variant="outlined" color="secondary" onClick={onClose} disabled={pending}>
            {t('form.cancel')}
          </Button>
          <Button type="submit" variant="contained" loading={create.isPending || update.isPending}>
            {creating ? t('form.create') : t('form.save')}
          </Button>
        </DialogActions>
      </Box>
    </Dialog>
  )
}

const visuallyHidden = {
  position: 'absolute',
  inlineSize: '1px',
  blockSize: '1px',
  overflow: 'hidden',
  clipPath: 'inset(50%)',
  whiteSpace: 'nowrap',
} as const
