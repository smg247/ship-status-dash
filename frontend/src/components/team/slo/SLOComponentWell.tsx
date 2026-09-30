import { Box, Card, styled, Typography } from '@mui/material'

import type { SLOComponentBlock } from '../../../types'
import { worstOutageStatus } from '../../../utils/helpers'
import { getStatusTintStyles } from '../../../utils/styles'

import { sloComponentDomId } from './format'
import SLOOutageCard from './SLOOutageCard'

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

const Meta = styled(Typography)(({ theme }) => ({
  color: theme.palette.text.secondary,
  fontSize: '0.875rem',
}))

interface SLOComponentWellProps {
  block: SLOComponentBlock
}

const SLOComponentWell = ({ block }: SLOComponentWellProps) => (
  <Section
    id={sloComponentDomId(block.component, block.sub_component)}
    status={worstOutageStatus(block.outages)}
  >
    <Title>
      {block.component} / {block.sub_component}
    </Title>
    {block.outages.length === 0 && <Meta>No active outages</Meta>}
    {block.outages.length > 0 && (
      <OutageList>
        {block.outages.map((outage) => (
          <SLOOutageCard key={outage.ID} outage={outage} showJira />
        ))}
      </OutageList>
    )}
  </Section>
)

export default SLOComponentWell
