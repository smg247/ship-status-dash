import { Box, Card, Link, styled, Typography } from '@mui/material'
import { useNavigate } from 'react-router'

import type { Outage, SLOComponentBlock } from '../../../types'
import { formatStatusSeverityText } from '../../../utils/helpers'
import { getStatusTintStyles } from '../../../utils/styles'
import { StatusChip } from '../../StatusColors'

import {
  openedLabel,
  outagePath,
  outageTintStatus,
  sloComponentDomId,
  worstOutageTint,
} from './format'

const Section = styled(Card)<{ status?: string }>(({ theme, status }) => ({
  ...(status ? getStatusTintStyles(theme, status, 2) : {}),
  borderRadius: theme.spacing(2),
  padding: theme.spacing(3),
  marginBottom: theme.spacing(3),
}))

const Title = styled(Typography)(({ theme }) => ({
  fontWeight: 600,
  fontSize: '1.25rem',
  marginBottom: theme.spacing(2),
}))

const OutageList = styled(Box)(({ theme }) => ({
  display: 'grid',
  gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 280px))',
  gap: theme.spacing(2),
  justifyContent: 'start',
}))

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

interface OutageWellProps {
  outage: Outage
}

const OutageWell = ({ outage }: OutageWellProps) => {
  const navigate = useNavigate()
  const jira = jiraURL(outage)

  return (
    <OutageCard
      severity={outageTintStatus(outage)}
      role="link"
      tabIndex={0}
      onClick={() => navigate(outagePath(outage))}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          navigate(outagePath(outage))
        }
      }}
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
          >
            {jira.label}
          </Link>
        </LinkRow>
      )}
    </OutageCard>
  )
}

interface SLOComponentWellProps {
  block: SLOComponentBlock
}

const SLOComponentWell = ({ block }: SLOComponentWellProps) => (
  <Section id={sloComponentDomId(block.component)} status={worstOutageTint(block.outages)}>
    <Title>{block.component}</Title>
    {block.outages.length === 0 && <Meta>No active outages</Meta>}
    {block.outages.length > 0 && (
      <OutageList>
        {block.outages.map((outage) => (
          <OutageWell key={outage.ID} outage={outage} />
        ))}
      </OutageList>
    )}
  </Section>
)

export default SLOComponentWell
