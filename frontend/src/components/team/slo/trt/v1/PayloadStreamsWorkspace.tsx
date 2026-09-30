import { Box, Button, Card, Chip, Link, styled, Typography } from '@mui/material'
import { useState } from 'react'

import type { SLOEvaluation, SLOItem, SLOWorkspace } from '../../../../../types'
import { getStatusChipColor } from '../../../../../utils/helpers'
import { getStatusTintStyles } from '../../../../../utils/styles'
import { formatAge, linkLabel, streamDomId } from '../../format'

import UpsertPayloadItemDialog from './UpsertPayloadItemDialog'

const streamPayloads = (rows: SLOItem[], stream: string): SLOItem[] =>
  rows
    .filter((item) => item.group_key === stream)
    .sort((a, b) => new Date(b.occurred_at).getTime() - new Date(a.occurred_at).getTime())

const payloadTintStatus = (outcome?: string): string | undefined => {
  switch (outcome) {
    case 'Accepted':
      return 'Healthy'
    case 'Rejected':
      return 'Down'
    default:
      return outcome ? 'Unknown' : undefined
  }
}

const Header = styled(Box)(({ theme }) => ({
  display: 'flex',
  justifyContent: 'space-between',
  alignItems: 'center',
  gap: theme.spacing(2),
  marginBottom: theme.spacing(2),
}))

const StreamWell = styled(Card, {
  shouldForwardProp: (prop) => prop !== 'status',
})<{ status?: string }>(({ theme, status }) => ({
  ...(status
    ? getStatusTintStyles(theme, status, 2)
    : { backgroundColor: theme.palette.background.paper }),
  borderRadius: theme.spacing(2),
  padding: theme.spacing(3),
  marginBottom: theme.spacing(3),
  border: status
    ? `1px solid ${getStatusChipColor(theme, status)}66`
    : `1px solid ${theme.palette.divider}`,
  scrollMarginTop: theme.spacing(10),
}))

const StreamHead = styled(Box)(({ theme }) => ({
  display: 'flex',
  justifyContent: 'space-between',
  alignItems: 'center',
  gap: theme.spacing(1),
  marginBottom: theme.spacing(1),
}))

const TableWrap = styled(Box)(({ theme }) => ({
  overflowX: 'auto',
  '& table': {
    width: '100%',
    borderCollapse: 'collapse',
  },
  '& th, & td': {
    textAlign: 'left',
    padding: theme.spacing(1),
    borderBottom: `1px solid ${theme.palette.divider}`,
    verticalAlign: 'top',
    fontSize: '0.875rem',
  },
}))

const JobNote = styled(Typography)(({ theme }) => ({
  color: theme.palette.text.secondary,
  fontSize: '0.75rem',
}))

const StreakChip = styled(Chip)(({ theme }) => ({
  marginLeft: theme.spacing(1),
}))

export interface PayloadStreamsWorkspaceProps {
  team: string
  workspace: SLOWorkspace
  evaluations: SLOEvaluation[]
  items: SLOItem[]
  canEdit: boolean
  onChanged: () => void
}

const PayloadStreamsWorkspace = ({
  team,
  workspace,
  evaluations,
  items,
  canEdit,
  onChanged,
}: PayloadStreamsWorkspaceProps) => {
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<SLOItem | undefined>(undefined)
  const groups = evaluations[0]?.groups ?? []
  const streams = workspace.streams ?? []

  return (
    <Box>
      <Header>
        <Typography variant="h6">Payload streams (amd64)</Typography>
        {canEdit && (
          <Button
            variant="outlined"
            onClick={() => {
              setEditing(undefined)
              setDialogOpen(true)
            }}
          >
            Add payload
          </Button>
        )}
      </Header>
      {streams.map((stream) => {
        const group = groups.find((item) => item.key === stream.name)
        const rows = streamPayloads(items, stream.name)
        return (
          <StreamWell
            key={stream.name}
            id={streamDomId(stream.name)}
            elevation={0}
            status={payloadTintStatus(rows[0]?.outcome)}
          >
            <StreamHead>
              <Typography fontWeight={600}>{stream.name}</Typography>
              {group && (
                <Chip
                  size="small"
                  color={group.met ? 'success' : 'warning'}
                  label={
                    group.met
                      ? `SLO met · last accepted ${formatAge(group.last_accepted_at)} ago`
                      : `SLO missed · last accepted ${formatAge(group.last_accepted_at)} ago`
                  }
                />
              )}
            </StreamHead>
            <TableWrap>
              <table>
                <thead>
                  <tr>
                    <th>Payload</th>
                    <th>Phase</th>
                    <th>Failed blocking jobs</th>
                    <th>Linked</th>
                    {canEdit && <th />}
                  </tr>
                </thead>
                <tbody>
                  {rows.length === 0 && (
                    <tr>
                      <td colSpan={canEdit ? 5 : 4}>No payloads stored</td>
                    </tr>
                  )}
                  {rows.map((row) => (
                    <tr key={row.item_key}>
                      <td>
                        <div>{row.item_key}</div>
                        {row.details.payload_url && (
                          <div>
                            <Link
                              href={row.details.payload_url}
                              target="_blank"
                              rel="noopener noreferrer"
                            >
                              Release controller
                            </Link>
                          </div>
                        )}
                        {row.details.analysis_url && (
                          <div>
                            <Link
                              href={row.details.analysis_url}
                              target="_blank"
                              rel="noopener noreferrer"
                            >
                              Payload agent
                            </Link>
                          </div>
                        )}
                      </td>
                      <td>
                        <Chip
                          size="small"
                          label={row.outcome}
                          color={row.outcome === 'Accepted' ? 'success' : 'error'}
                        />
                      </td>
                      <td>
                        {(row.details.jobs ?? []).length === 0 && '-'}
                        <ul>
                          {(row.details.jobs ?? []).map((job) => (
                            <li key={job.name}>
                              <Link href={job.url} target="_blank" rel="noopener noreferrer">
                                {job.name}
                              </Link>
                              {job.recurring_count !== undefined && job.recurring_count >= 2 && (
                                <StreakChip
                                  size="small"
                                  color="warning"
                                  label={`${job.recurring_count} Payload Streak`}
                                />
                              )}
                              {job.notes && <JobNote>{job.notes}</JobNote>}
                            </li>
                          ))}
                        </ul>
                      </td>
                      <td>
                        {(row.links ?? []).map((link) => (
                          <div key={link.ID}>
                            <Link href={link.url} target="_blank" rel="noopener noreferrer">
                              {linkLabel(link)}
                            </Link>
                          </div>
                        ))}
                        {row.notes && <JobNote>{row.notes}</JobNote>}
                      </td>
                      {canEdit && (
                        <td>
                          <Button
                            size="small"
                            onClick={() => {
                              setEditing(row)
                              setDialogOpen(true)
                            }}
                          >
                            Edit
                          </Button>
                        </td>
                      )}
                    </tr>
                  ))}
                </tbody>
              </table>
            </TableWrap>
          </StreamWell>
        )
      })}
      {canEdit && (
        <UpsertPayloadItemDialog
          key={`${dialogOpen}-${editing?.item_key ?? 'new'}`}
          open={dialogOpen}
          team={team}
          streams={streams.map((stream) => stream.name)}
          item={editing}
          onClose={() => setDialogOpen(false)}
          onSuccess={() => {
            setDialogOpen(false)
            onChanged()
          }}
        />
      )}
    </Box>
  )
}

export default PayloadStreamsWorkspace
