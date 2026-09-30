import { ArrowBack } from '@mui/icons-material'
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Container,
  Paper,
  styled,
  Typography,
} from '@mui/material'
import { useEffect, useMemo, useRef } from 'react'
import { useNavigate, useParams } from 'react-router'

import { useAuth } from '../../contexts/AuthContext'
import useAbortableGet from '../../hooks/useAbortableGet'
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

const SLOLoading = styled(Box)(({ theme }) => ({
  display: 'flex',
  justifyContent: 'center',
  marginBottom: theme.spacing(3),
}))

const SLOError = styled(Alert)(({ theme }) => ({
  marginBottom: theme.spacing(2),
}))

const TeamPage = () => {
  const navigate = useNavigate()
  const { team } = useParams<{ team: string }>()
  const decodedTeam = team ? decodeURIComponent(team) : ''
  const teamColor = decodedTeam ? getTeamColor(decodedTeam) : undefined
  const filters = useMemo(() => ({ team: decodedTeam }), [decodedTeam])
  const { isTeamSLOAdmin } = useAuth()
  const {
    data: slo,
    loading: sloLoading,
    error: sloError,
    reload: reloadSLO,
  } = useAbortableGet<TeamSLO>(decodedTeam ? getTeamSLOEndpoint(decodedTeam) : null)
  const currentSLO = slo?.team === decodedTeam ? slo : null
  const hashScrolled = useRef('')

  useIntervalRefresh(() => reloadSLO(true), undefined, decodedTeam !== '')

  useEffect(() => {
    if (!currentSLO) {
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
  }, [currentSLO, decodedTeam])

  if (!team) return null

  const hasEvaluations = (currentSLO?.evaluations.length ?? 0) > 0
  const hasComponents = (currentSLO?.slo_components.length ?? 0) > 0
  const workspace = currentSLO?.workspace
  const workspaceItems =
    currentSLO?.items.filter(
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

      {sloError && <SLOError severity="error">{sloError}</SLOError>}
      {sloLoading && !currentSLO && (
        <SLOLoading>
          <CircularProgress />
        </SLOLoading>
      )}
      {hasEvaluations && currentSLO && (
        <TeamSLOStatus
          team={decodedTeam}
          evaluations={currentSLO.evaluations}
          sloComponents={currentSLO.slo_components}
        />
      )}
      {hasComponents &&
        currentSLO?.slo_components.map((block) => (
          <SLOComponentWell key={`${block.component}-${block.sub_component}`} block={block} />
        ))}
      {workspace &&
        currentSLO &&
        renderTeamWorkspace({
          team: decodedTeam,
          workspace,
          evaluations: currentSLO.evaluations,
          items: workspaceItems,
          canEdit: isTeamSLOAdmin(decodedTeam),
          onChanged: () => reloadSLO(true),
        })}

      <SectionTitle>Sub components</SectionTitle>
      <SubComponentList filters={filters} />
    </StyledContainer>
  )
}

export default TeamPage
