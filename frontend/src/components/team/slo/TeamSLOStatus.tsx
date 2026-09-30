import { Box, Chip, styled, Typography } from '@mui/material'

import type { SLOComponentBlock, SLOEvaluation } from '../../../types'

import { formatAge, worstMiss } from './format'

const Section = styled(Box)(({ theme }) => ({
  backgroundColor: theme.palette.background.paper,
  borderRadius: theme.spacing(2),
  padding: theme.spacing(3),
  marginBottom: theme.spacing(3),
  border: `1px solid ${theme.palette.divider}`,
}))

const Metrics = styled(Box)(({ theme }) => ({
  display: 'flex',
  flexWrap: 'wrap',
  gap: theme.spacing(3),
  marginBottom: theme.spacing(2),
}))

const MetricLabel = styled(Typography)(({ theme }) => ({
  fontSize: '0.8rem',
  color: theme.palette.text.secondary,
}))

const SectionTitle = styled(Typography)(({ theme }) => ({
  fontWeight: 600,
  fontSize: '1.25rem',
  marginBottom: theme.spacing(2),
}))

const MetricValue = styled(Typography)({
  fontSize: '1.5rem',
  fontWeight: 600,
})

const WorstMissValue = styled(MetricValue)({
  fontSize: '1.1rem',
})

const ChipRow = styled(Box)(({ theme }) => ({
  display: 'flex',
  flexWrap: 'wrap',
  gap: theme.spacing(1),
}))

interface TeamSLOStatusProps {
  team: string
  evaluations: SLOEvaluation[]
  sloComponents: SLOComponentBlock[]
}

const TeamSLOStatus = ({ team, evaluations, sloComponents }: TeamSLOStatusProps) => {
  const incidentCount = sloComponents.reduce((sum, block) => sum + block.outages.length, 0)

  return (
    <Section id="slo">
      <SectionTitle>{team} SLOs</SectionTitle>
      {evaluations.map((evaluation) => {
        const metCount = evaluation.groups.filter((group) => group.met).length
        const miss = worstMiss(evaluation)
        return (
          <Box key={evaluation.name}>
            <Metrics>
              <Box>
                <MetricLabel>{evaluation.display_name || evaluation.name}</MetricLabel>
                <MetricValue>
                  {metCount} of {evaluation.groups.length}
                </MetricValue>
                <Chip
                  size="small"
                  label={evaluation.met ? 'Met' : 'Missed'}
                  color={evaluation.met ? 'success' : 'warning'}
                />
              </Box>
              {miss && (
                <Box>
                  <MetricLabel>Worst miss</MetricLabel>
                  <WorstMissValue>{miss.key}</WorstMissValue>
                  <MetricLabel>
                    {miss.last_accepted_at
                      ? `Last accepted ${formatAge(miss.last_accepted_at)} ago`
                      : 'No accepted payload stored'}
                  </MetricLabel>
                </Box>
              )}
              {incidentCount > 0 && (
                <Box>
                  <MetricLabel>Open incidents</MetricLabel>
                  <MetricValue>{incidentCount}</MetricValue>
                </Box>
              )}
            </Metrics>
            <ChipRow>
              {evaluation.groups.map((group) => (
                <Chip
                  key={group.key}
                  size="small"
                  label={`${group.key} · ${formatAge(group.last_accepted_at)}`}
                  color={group.met ? 'success' : 'warning'}
                />
              ))}
            </ChipRow>
          </Box>
        )
      })}
    </Section>
  )
}

export default TeamSLOStatus
