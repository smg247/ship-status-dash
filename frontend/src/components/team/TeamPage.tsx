import { ArrowBack } from '@mui/icons-material'
import { Button, Container, Paper, styled, Typography } from '@mui/material'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router'

import { useAuth } from '../../contexts/AuthContext'
import useIntervalRefresh from '../../hooks/useIntervalRefresh'
import type { TeamSLO } from '../../types'
import { getTeamSLOEndpoint } from '../../utils/endpoints'
import { getTeamColor } from '../../utils/teamColor'
import SubComponentList from '../sub-component/SubComponentList'

import { renderTeamWorkspace } from './slo/registry'
import SLOComponentWell from './slo/SLOComponentWell'
import TeamSLOStatus from './slo/TeamSLOStatus'

const StyledContainer = styled(Container)(({ theme }) => ({
  marginTop: theme.spacing(4),
  marginBottom: theme.spacing(4),
}))

const BackButton = styled(Button)(({ theme }) => ({
  marginBottom: theme.spacing(3),
}))

const TeamHeader = styled(Paper, {
  shouldForwardProp: (prop) => prop !== 'teamColor',
})<{ teamColor?: string }>(({ theme, teamColor }) => {
  const isDark = theme.palette.mode === 'dark'
  return {
    padding: theme.spacing(4),
    marginBottom: theme.spacing(4),
    borderRadius: theme.spacing(2),
    backgroundColor: teamColor
      ? `${teamColor}${isDark ? '25' : '15'}`
      : isDark
        ? theme.palette.grey[800]
        : theme.palette.grey[100],
    border: teamColor ? `2px solid ${teamColor}40` : undefined,
  }
})

const TeamTitle = styled(Typography)(({ theme }) => ({
  fontWeight: 600,
  fontSize: '2rem',
  color: theme.palette.text.primary,
  [theme.breakpoints.down('md')]: {
    fontSize: '1.75rem',
  },
  [theme.breakpoints.down('sm')]: {
    fontSize: '1.5rem',
  },
}))

const SectionTitle = styled(Typography)(({ theme }) => ({
  fontWeight: 600,
  fontSize: '1.25rem',
  margin: theme.spacing(1, 0, 2),
}))

const TeamPage = () => {
  const navigate = useNavigate()
  const { team } = useParams<{ team: string }>()
  const decodedTeam = team ? decodeURIComponent(team) : ''
  const teamColor = decodedTeam ? getTeamColor(decodedTeam) : undefined
  const filters = useMemo(() => ({ team: decodedTeam }), [decodedTeam])
  const { isTeamSLOAdmin } = useAuth()
  const [slo, setSlo] = useState<TeamSLO | null>(null)
  const hashScrolled = useRef('')

  const loadSLO = useCallback(() => {
    if (!decodedTeam) {
      return
    }
    fetch(getTeamSLOEndpoint(decodedTeam))
      .then((response) => (response.ok ? response.json() : null))
      .then((data: TeamSLO | null) => setSlo(data))
      .catch(() => setSlo(null))
  }, [decodedTeam])

  useEffect(() => {
    loadSLO()
  }, [loadSLO])

  useIntervalRefresh(loadSLO, undefined, decodedTeam !== '')

  useEffect(() => {
    if (!slo) {
      return
    }
    const id = decodeURIComponent(window.location.hash.replace('#', ''))
    const token = `${decodedTeam}:${id}`
    if (!id || hashScrolled.current === token) {
      return
    }
    const target = document.getElementById(id)
    if (!target) {
      return
    }
    target.scrollIntoView()
    hashScrolled.current = token
  }, [slo, decodedTeam])

  if (!team) return null

  const hasEvaluations = (slo?.evaluations.length ?? 0) > 0
  const hasComponents = (slo?.slo_components.length ?? 0) > 0
  const workspace = slo?.workspace
  const workspaceItems =
    slo?.items.filter(
      (item) => item.kind === workspace?.kind && item.schema_version === workspace?.schema_version,
    ) ?? []

  return (
    <StyledContainer maxWidth="lg">
      <BackButton variant="outlined" startIcon={<ArrowBack />} onClick={() => navigate('/')}>
        Main Dashboard
      </BackButton>

      <TeamHeader elevation={2} teamColor={teamColor}>
        <TeamTitle>{decodedTeam} Dashboard</TeamTitle>
      </TeamHeader>

      {hasEvaluations && slo && (
        <TeamSLOStatus
          team={decodedTeam}
          evaluations={slo.evaluations}
          sloComponents={slo.slo_components}
        />
      )}
      {hasComponents &&
        slo?.slo_components.map((block) => (
          <SLOComponentWell key={`${block.component}-${block.sub_component}`} block={block} />
        ))}
      {workspace &&
        renderTeamWorkspace({
          team: decodedTeam,
          workspace,
          evaluations: slo?.evaluations ?? [],
          items: workspaceItems,
          canEdit: isTeamSLOAdmin(decodedTeam),
          onChanged: loadSLO,
        })}

      <SectionTitle>Sub components</SectionTitle>
      <SubComponentList filters={filters} />
    </StyledContainer>
  )
}

export default TeamPage
