import { Box, Card, Link, styled, Typography } from '@mui/material'
import type { KeyboardEvent, MouseEvent } from 'react'
import { useNavigate } from 'react-router'

import type { Outage } from '../../../types'
import { formatStatusSeverityText, outageStatus } from '../../../utils/helpers'
import { getStatusTintStyles } from '../../../utils/styles'
import { StatusChip } from '../../StatusColors'

import { openedLabel, outagePath } from './format'

const OutageCard = styled(Card)<{ severity: string }>(({ theme, severity }) => ({
  ...getStatusTintStyles(theme, severity, 1.5),
  ...(theme.palette.mode === 'dark' && { backgroundColor: theme.palette.grey[900] }),
  borderRadius: theme.spacing(1.5),
  padding: theme.spacing(2.5),
  minHeight: 160,
  height: '100%',
  cursor: 'pointer',
  transition: 'all 0.2s ease-in-out',
  '&:hover': {
    boxShadow: theme.shadows[4],
    transform: 'translateY(-1px)',
  },
}))

const SeverityRow = styled(Box)(({ theme }) => ({
  marginBottom: theme.spacing(1),
}))

const Meta = styled(Typography)(({ theme }) => ({
  color: theme.palette.text.secondary,
  fontSize: '0.875rem',
}))

const LinkRow = styled(Box)(({ theme }) => ({
  display: 'flex',
  flexWrap: 'wrap',
  gap: theme.spacing(2),
  marginTop: theme.spacing(1),
}))

const jiraURL = (outage: Outage): { label: string; href: string } | undefined => {
  const link = outage.links?.find((item) => item.link_type === 'jira')
  if (link) {
    return { label: link.description || 'Jira', href: link.url }
  }
  const reason = outage.reasons?.find((item) => item.type === 'jira' && item.check)
  if (reason) {
    return {
      label: reason.check,
      href: `https://redhat.atlassian.net/browse/${reason.check}`,
    }
  }
  return undefined
}

interface SLOOutageCardProps {
  outage: Outage
  showJira?: boolean
  stopPropagation?: boolean
}

const SLOOutageCard = ({
  outage,
  showJira = false,
  stopPropagation = false,
}: SLOOutageCardProps) => {
  const navigate = useNavigate()
  const jira = showJira ? jiraURL(outage) : undefined
  const open = () => navigate(outagePath(outage))

  const handleClick = (event: MouseEvent) => {
    if (stopPropagation) {
      event.stopPropagation()
    }
    open()
  }

  const handleKeyDown = (event: KeyboardEvent) => {
    if (event.target !== event.currentTarget) {
      return
    }
    if (event.key !== 'Enter' && event.key !== ' ') {
      return
    }
    event.preventDefault()
    if (stopPropagation) {
      event.stopPropagation()
    }
    open()
  }

  return (
    <OutageCard
      severity={outageStatus(outage)}
      role="link"
      tabIndex={0}
      onClick={handleClick}
      onKeyDown={handleKeyDown}
    >
      <SeverityRow>
        <StatusChip
          size="small"
          label={formatStatusSeverityText(outage.severity)}
          status={outage.severity}
          variant="filled"
        />
      </SeverityRow>
      <Typography variant="subtitle1">{outage.description || 'Outage'}</Typography>
      <Meta>
        {openedLabel(outage.start_time)}
        {outage.discovered_from ? ` · discovered by ${outage.discovered_from}` : ''}
      </Meta>
      {jira && (
        <LinkRow>
          <Link
            href={jira.href}
            target="_blank"
            rel="noopener noreferrer"
            onClick={(event) => event.stopPropagation()}
            onKeyDown={(event) => event.stopPropagation()}
          >
            {jira.label}
          </Link>
        </LinkRow>
      )}
    </OutageCard>
  )
}

export default SLOOutageCard
