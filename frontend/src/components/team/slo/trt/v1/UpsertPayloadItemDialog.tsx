import {
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  MenuItem,
  styled,
  TextField,
  Typography,
} from '@mui/material'
import { useState } from 'react'

import type { SLOItem, SLOItemLink, SLOJob } from '../../../../../types'
import {
  deleteSLOItemLinkEndpoint,
  putSLOItemEndpoint,
  putSLOItemLinkEndpoint,
} from '../../../../../utils/endpoints'
import { formatDateForDateTimeLocal } from '../../../../../utils/helpers'

const PAYLOAD_STREAMS_KIND = 'payload_streams'
const SCHEMA_VERSION = 1

const Content = styled(DialogContent)(({ theme }) => ({
  paddingTop: theme.spacing(1),
}))

const Field = styled(TextField)(({ theme }) => ({
  marginBottom: theme.spacing(2),
}))

const Section = styled('section')(({ theme }) => ({
  marginTop: theme.spacing(1),
  marginBottom: theme.spacing(3),
}))

const SectionTitle = styled(Typography)(({ theme }) => ({
  fontWeight: 600,
  fontSize: '1rem',
  marginBottom: theme.spacing(2),
  paddingBottom: theme.spacing(1),
  borderBottom: `1px solid ${theme.palette.divider}`,
}))

const Entry = styled('div')(({ theme }) => ({
  border: `1px solid ${theme.palette.divider}`,
  borderRadius: theme.spacing(1),
  padding: theme.spacing(2),
  marginBottom: theme.spacing(2),
  backgroundColor: theme.palette.background.default,
}))

const EntryActions = styled('div')({
  display: 'flex',
  justifyContent: 'flex-end',
})

const ErrorText = styled('p')(({ theme }) => ({
  color: theme.palette.error.main,
  margin: theme.spacing(1, 0),
}))

interface JobDraft {
  draftId: string
  name: string
  url: string
  notes: string
  recurring_count?: number
}

interface LinkDraft {
  id?: number
  url: string
  link_type: SLOItemLink['link_type']
}

interface UpsertPayloadItemDialogProps {
  open: boolean
  team: string
  streams: string[]
  item?: SLOItem
  onClose: () => void
  onSuccess: () => void
}

let jobDraftSeq = 0

const newJobDraft = (partial?: Partial<JobDraft>): JobDraft => {
  jobDraftSeq += 1
  return {
    draftId: `job-${jobDraftSeq}`,
    name: partial?.name ?? '',
    url: partial?.url ?? '',
    notes: partial?.notes ?? '',
    recurring_count: partial?.recurring_count,
  }
}

const emptyLink = (): LinkDraft => ({ url: '', link_type: 'jira' })

const initialLinks = (item?: SLOItem): LinkDraft[] => {
  const existing = (item?.links ?? []).map((link) => ({
    id: link.ID,
    url: link.url,
    link_type: link.link_type,
  }))
  return existing.length > 0 ? existing : [emptyLink()]
}

const UpsertPayloadItemDialog = ({
  open,
  team,
  streams,
  item,
  onClose,
  onSuccess,
}: UpsertPayloadItemDialogProps) => {
  const editing = !!item
  const [stream, setStream] = useState(item?.group_key ?? streams[0] ?? '')
  const [tag, setTag] = useState(item?.item_key ?? '')
  const [occurredAt, setOccurredAt] = useState(
    item
      ? formatDateForDateTimeLocal(new Date(item.occurred_at))
      : formatDateForDateTimeLocal(new Date()),
  )
  const [outcome, setOutcome] = useState(item?.outcome ?? 'Rejected')
  const [payloadURL, setPayloadURL] = useState(item?.details.payload_url ?? '')
  const [analysisURL, setAnalysisURL] = useState(item?.details.analysis_url ?? '')
  const [notes, setNotes] = useState(item?.notes ?? '')
  const [jobs, setJobs] = useState<JobDraft[]>(
    (item?.details.jobs ?? []).map((job) => newJobDraft(job)),
  )
  const [links, setLinks] = useState<LinkDraft[]>(initialLinks(item))
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const updateJob = (index: number, patch: Partial<JobDraft>) => {
    setJobs((current) => current.map((job, i) => (i === index ? { ...job, ...patch } : job)))
  }

  const updateLink = (index: number, patch: Partial<LinkDraft>) => {
    setLinks((current) => current.map((link, i) => (i === index ? { ...link, ...patch } : link)))
  }

  const readError = async (response: Response, fallback: string) => {
    const payload = (await response.json().catch(() => null)) as { error?: string } | null
    return payload?.error || fallback
  }

  const syncLinks = async (saved: SLOItem) => {
    const original = item?.links ?? []
    const desired = links
      .map((link) => ({ ...link, url: link.url.trim() }))
      .filter((link) => link.url !== '')

    for (const existing of original) {
      const kept = desired.some(
        (link) =>
          link.id === existing.ID &&
          link.url === existing.url &&
          link.link_type === existing.link_type,
      )
      if (kept) {
        continue
      }
      const response = await fetch(
        deleteSLOItemLinkEndpoint(team, saved.kind, saved.item_key, existing.ID),
        { method: 'DELETE', credentials: 'include' },
      )
      if (!response.ok && response.status !== 404) {
        setError(await readError(response, `Link delete failed (${response.status})`))
        return false
      }
    }

    for (const link of desired) {
      const unchanged = original.some(
        (existing) =>
          existing.ID === link.id &&
          existing.url === link.url &&
          existing.link_type === link.link_type,
      )
      if (unchanged) {
        continue
      }
      const response = await fetch(putSLOItemLinkEndpoint(team, saved.kind, saved.item_key), {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url: link.url, link_type: link.link_type }),
      })
      if (!response.ok) {
        setError(await readError(response, `Link failed (${response.status})`))
        return false
      }
    }
    return true
  }

  const submit = async () => {
    const occurred = new Date(occurredAt)
    if (occurredAt.trim() === '' || Number.isNaN(occurred.getTime())) {
      setError('Occurred at must be a valid time')
      return
    }
    setSaving(true)
    setError('')
    let succeeded = false
    try {
      const bodyJobs: SLOJob[] = jobs
        .filter((job) => job.name.trim() !== '')
        .map((job) => ({
          name: job.name.trim(),
          url: job.url.trim(),
          state: 'failure',
          notes: job.notes,
          ...(job.recurring_count !== undefined ? { recurring_count: job.recurring_count } : {}),
        }))
      const response = await fetch(putSLOItemEndpoint(team), {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          kind: PAYLOAD_STREAMS_KIND,
          schema_version: SCHEMA_VERSION,
          item_key: tag.trim(),
          group_key: stream,
          occurred_at: occurred.toISOString(),
          outcome,
          notes,
          details: {
            payload_url: payloadURL.trim(),
            ...(analysisURL.trim() ? { analysis_url: analysisURL.trim() } : {}),
            jobs: bodyJobs,
          },
        }),
      })
      if (!response.ok) {
        setError(await readError(response, `Save failed (${response.status})`))
        return
      }
      const saved = (await response.json()) as SLOItem
      if (!(await syncLinks(saved))) {
        return
      }
      succeeded = true
    } catch {
      setError('Save failed')
    } finally {
      setSaving(false)
    }
    if (succeeded) {
      onSuccess()
    }
  }

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle>{editing ? 'Edit payload' : 'Add payload'}</DialogTitle>
      <Content>
        <Section>
          <SectionTitle>Payload</SectionTitle>
          <Field
            select
            fullWidth
            label="Stream"
            value={stream}
            disabled={editing}
            onChange={(event) => setStream(event.target.value)}
          >
            {streams.map((name) => (
              <MenuItem key={name} value={name}>
                {name}
              </MenuItem>
            ))}
          </Field>
          <Field
            fullWidth
            label="Tag"
            value={tag}
            disabled={editing}
            onChange={(event) => setTag(event.target.value)}
          />
          <Field
            fullWidth
            label="Occurred at"
            type="datetime-local"
            value={occurredAt}
            onChange={(event) => setOccurredAt(event.target.value)}
            slotProps={{ inputLabel: { shrink: true } }}
          />
          <Field
            select
            fullWidth
            label="Phase"
            value={outcome}
            onChange={(event) => setOutcome(event.target.value)}
          >
            {['Accepted', 'Rejected', 'Ready'].map((value) => (
              <MenuItem key={value} value={value}>
                {value}
              </MenuItem>
            ))}
          </Field>
          <Field
            fullWidth
            label="Release controller URL"
            value={payloadURL}
            onChange={(event) => setPayloadURL(event.target.value)}
          />
          <Field
            fullWidth
            label="Payload agent URL"
            value={analysisURL}
            onChange={(event) => setAnalysisURL(event.target.value)}
          />
          <Field
            fullWidth
            label="Payload notes"
            value={notes}
            multiline
            onChange={(event) => setNotes(event.target.value)}
          />
        </Section>
        <Section>
          <SectionTitle>Failed jobs</SectionTitle>
          {jobs.map((job, index) => (
            <Entry key={job.draftId}>
              <Field
                fullWidth
                label="Job name"
                value={job.name}
                onChange={(event) => updateJob(index, { name: event.target.value })}
              />
              <Field
                fullWidth
                label="Job URL"
                value={job.url}
                onChange={(event) => updateJob(index, { url: event.target.value })}
              />
              <Field
                fullWidth
                label="Job notes"
                value={job.notes}
                onChange={(event) => updateJob(index, { notes: event.target.value })}
              />
              <EntryActions>
                <Button
                  variant="outlined"
                  color="error"
                  onClick={() => setJobs((current) => current.filter((_, i) => i !== index))}
                >
                  Remove job
                </Button>
              </EntryActions>
            </Entry>
          ))}
          <Button
            variant="outlined"
            color="primary"
            onClick={() => setJobs((current) => [...current, newJobDraft()])}
          >
            Add job
          </Button>
        </Section>
        <Section>
          <SectionTitle>Links</SectionTitle>
          {links.map((link, index) => (
            <Entry key={link.id ?? `new-${index}`}>
              <Field
                fullWidth
                label="Link URL"
                value={link.url}
                onChange={(event) => updateLink(index, { url: event.target.value })}
              />
              <Field
                select
                fullWidth
                label="Link type"
                value={link.link_type}
                onChange={(event) =>
                  updateLink(index, { link_type: event.target.value as LinkDraft['link_type'] })
                }
              >
                {(['jira', 'outage', 'other'] as const).map((value) => (
                  <MenuItem key={value} value={value}>
                    {value}
                  </MenuItem>
                ))}
              </Field>
              <EntryActions>
                <Button
                  variant="outlined"
                  color="error"
                  onClick={() => setLinks((current) => current.filter((_, i) => i !== index))}
                >
                  Remove link
                </Button>
              </EntryActions>
            </Entry>
          ))}
          <Button
            variant="outlined"
            color="primary"
            onClick={() => setLinks((current) => [...current, emptyLink()])}
          >
            Add link
          </Button>
        </Section>
        {error && <ErrorText>{error}</ErrorText>}
      </Content>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button
          variant="contained"
          disabled={saving || !tag.trim() || !payloadURL.trim() || occurredAt.trim() === ''}
          onClick={() => void submit()}
        >
          Save
        </Button>
      </DialogActions>
    </Dialog>
  )
}

export default UpsertPayloadItemDialog
