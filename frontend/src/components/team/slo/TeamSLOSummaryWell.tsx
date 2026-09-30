import { Box, Card, styled, Typography } from '@mui/material'
import type { KeyboardEvent, MouseEvent } from 'react'
import { useNavigate } from 'react-router'

import type { Outage, SLOComponentBlock, SLOEvaluation, TeamSLOSummary } from '../../../types'
import { formatStatusSeverityText, outageStatus, worstOutageStatus } from '../../../utils/helpers'
import { getStatusTintStyles } from '../../../utils/styles'
import { StatusChip } from '../../StatusColors'

import { formatAge, openedLabel, outagePath, streamDomId } from './format'
import { trtPayloadResult } from './trt/v1/result'

const Section = styled(Box)(({ theme }) => ({
  marginBottom: theme.spacing(5),
  paddingBottom: theme.spacing(5),
  borderBottom: `2px solid ${theme.palette.divider}`,
}))

const HeaderBox = styled(Box)(({ theme }) => ({
  display: 'flex',
  justifyContent: 'space-between',
  alignItems: 'center',
  gap: theme.spacing(2),
  marginBottom: theme.spacing(2),
  paddingBottom: theme.spacing(2),
  borderBottom: `1px solid ${theme.palette.divider}`,
}))

const WellTitle = styled(Typography)(({ theme }) => ({
  fontWeight: 600,
  fontSize: '1.5rem',
  color: theme.palette.text.primary,
}))

const TeamTitle = styled(Typography)(({ theme }) => ({
  fontWeight: 600,
  fontSize: '1.25rem',
  color: theme.palette.text.primary,
}))

const ComponentTitle = styled(Typography)(({ theme }) => ({
  fontWeight: 600,
  fontSize: '1rem',
  marginBottom: theme.spacing(1.5),
}))

const Stack = styled(Box)(({ theme }) => ({
  display: 'flex',
  flexDirection: 'column',
  gap: theme.spacing(2),
}))

const IncidentGrid = styled(Box)(({ theme }) => ({
  display: 'grid',
  gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 280px))',
  gap: theme.spacing(2),
  justifyContent: 'start',
}))

const Meta = styled(Typography)(({ theme }) => ({
  color: theme.palette.text.secondary,
  fontSize: '0.875rem',
  marginBottom: theme.spacing(1.5),
}))

const ChipRow = styled(Box)(({ theme }) => ({
  display: 'flex',
  flexWrap: 'wrap',
  gap: theme.spacing(1),
  marginBottom: theme.spacing(2),
}))

const ClickableWell = styled(Card)<{ severity: string }>(({ theme, severity }) => ({
  ...getStatusTintStyles(theme, severity, 1.5),
  ...(theme.palette.mode === 'dark' && { backgroundColor: theme.palette.grey[900] }),
  borderRadius: theme.spacing(1.5),
  padding: theme.spacing(2),
  cursor: 'pointer',
  transition: 'all 0.2s ease-in-out',
  '&:hover': {
    boxShadow: theme.shadows[4],
    transform: 'translateY(-1px)',
  },
}))

const IncidentCard = styled(ClickableWell)(({ theme }) => ({
  minHeight: 160,
  height: '100%',
  padding: theme.spacing(2.5),
}))

const CardMeta = styled(Meta)({
  marginBottom: 0,
})

const NestedWell = styled(Card)<{ status?: string }>(({ theme, status }) => ({
  ...(status
    ? getStatusTintStyles(theme, status, 1.5)
    : { backgroundColor: theme.palette.background.paper }),
  ...(theme.palette.mode === 'dark' && { backgroundColor: theme.palette.grey[900] }),
  borderRadius: theme.spacing(1.5),
  padding: theme.spacing(2),
}))

const SeverityRow = styled(Box)(({ theme }) => ({
  marginBottom: theme.spacing(1),
}))

const sloStatus = (met: boolean | undefined) => {
  if (met === undefined) {
    return 'Unknown'
  }
  return met ? 'Healthy' : 'Degraded'
}

const activateOnKey = (event: KeyboardEvent, action: () => void) => {
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    event.stopPropagation()
    action()
  }
}

interface OutageSummaryWellProps {
  outage: Outage
}

const OutageSummaryWell = ({ outage }: OutageSummaryWellProps) => {
  const navigate = useNavigate()
  const open = () => navigate(outagePath(outage))

  return (
    <IncidentCard
      severity={outageStatus(outage)}
      role="link"
      tabIndex={0}
      onClick={(event: MouseEvent) => {
        event.stopPropagation()
        open()
      }}
      onKeyDown={(event) => activateOnKey(event, open)}
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
      <CardMeta>
        {openedLabel(outage.start_time)}
        {outage.discovered_from ? ` · discovered by ${outage.discovered_from}` : ''}
      </CardMeta>
    </IncidentCard>
  )
}

interface ComponentSummaryWellProps {
  block: SLOComponentBlock
}

const ComponentSummaryWell = ({ block }: ComponentSummaryWellProps) => (
  <NestedWell status={worstOutageStatus(block.outages)}>
    <ComponentTitle>{block.sub_component}</ComponentTitle>
    {block.outages.length === 0 && <Meta>No active outages</Meta>}
    {block.outages.length > 0 && (
      <IncidentGrid>
        {block.outages.map((outage) => (
          <OutageSummaryWell key={outage.ID} outage={outage} />
        ))}
      </IncidentGrid>
    )}
  </NestedWell>
)

interface TeamSummaryWellProps {
  team: string
  evaluation?: SLOEvaluation
  components: SLOComponentBlock[]
}

const TeamSummaryWell = ({ team, evaluation, components }: TeamSummaryWellProps) => {
  const navigate = useNavigate()
  const status = sloStatus(evaluation?.met)
  const result = trtPayloadResult(evaluation)
  const missedCount = result ? result.groups.filter((group) => !group.met).length : 0
  const open = () => navigate(`/team/${encodeURIComponent(team)}`)

  return (
    <ClickableWell
      severity={status}
      role="link"
      tabIndex={0}
      onClick={open}
      onKeyDown={(event) => activateOnKey(event, open)}
    >
      <HeaderBox>
        <TeamTitle>{team} SLO</TeamTitle>
        {evaluation && (
          <StatusChip
            size="small"
            status={status}
            variant="filled"
            label={
              result
                ? evaluation.met
                  ? `All ${result.groups.length} met`
                  : `${missedCount} of ${result.groups.length} missed`
                : evaluation.met
                  ? 'Met'
                  : 'Missed'
            }
          />
        )}
      </HeaderBox>
      {result && result.groups.length > 0 && (
        <ChipRow>
          {result.groups.map((group) => (
            <StatusChip
              key={group.key}
              size="small"
              status={group.met ? 'Healthy' : 'Degraded'}
              variant="filled"
              label={`${group.key} · ${formatAge(group.last_accepted_at)}`}
              onClick={(event) => {
                event.stopPropagation()
                navigate(`/team/${encodeURIComponent(team)}#${streamDomId(group.key)}`)
              }}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.stopPropagation()
                }
              }}
            />
          ))}
        </ChipRow>
      )}
      {components.length > 0 && (
        <Stack>
          {components.map((block) => (
            <ComponentSummaryWell key={`${block.component}-${block.sub_component}`} block={block} />
          ))}
        </Stack>
      )}
    </ClickableWell>
  )
}

interface TeamSLOSummaryWellProps {
  summary: TeamSLOSummary
}

const TeamSLOSummaryWell = ({ summary }: TeamSLOSummaryWellProps) => {
  const anyEvaluation = summary.teams.some((block) => block.evaluations.length > 0)
  const anyMissed = summary.teams.some((block) =>
    block.evaluations.some((evaluation) => !evaluation.met),
  )
  const outerStatus = !anyEvaluation ? 'Unknown' : anyMissed ? 'Degraded' : 'Healthy'

  return (
    <Section>
      <HeaderBox>
        <WellTitle>SHIP Teams</WellTitle>
        <StatusChip
          label={
            outerStatus === 'Healthy' ? 'Met' : outerStatus === 'Degraded' ? 'Missed' : 'Unknown'
          }
          status={outerStatus}
          variant="filled"
        />
      </HeaderBox>
      <Stack>
        {summary.teams.map((block) => (
          <TeamSummaryWell
            key={block.team}
            team={block.team}
            evaluation={block.evaluations[0]}
            components={block.slo_components}
          />
        ))}
      </Stack>
    </Section>
  )
}

export default TeamSLOSummaryWell
