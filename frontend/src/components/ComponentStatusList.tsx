import { Alert, Box, CircularProgress, Container, styled, Typography } from '@mui/material'
import React, { useCallback, useEffect, useState } from 'react'

import useAbortableGet from '../hooks/useAbortableGet'
import useIntervalRefresh from '../hooks/useIntervalRefresh'
import type { Component, TeamSLOSummary } from '../types'
import { deferMountFetch } from '../utils/deferMountFetch'
import {
  getComponentsEndpoint,
  getOverallStatusEndpoint,
  getTeamSLOSummaryEndpoint,
} from '../utils/endpoints'
import { slugify } from '../utils/slugify'

import ComponentWell from './component/ComponentWell'
import UnhealthyWell from './component/UnhealthyWell'
import TeamSLOSummaryWell from './team/slo/TeamSLOSummaryWell'

const StyledContainer = styled(Container)(({ theme }) => ({
  marginTop: theme.spacing(4),
}))

const LoadingBox = styled(Box)(() => ({
  display: 'flex',
  justifyContent: 'center',
  alignItems: 'center',
  minHeight: '200px',
}))

const TitleSection = styled(Box)(({ theme }) => ({
  padding: theme.spacing(1, 0),
  marginBottom: theme.spacing(4),
  textAlign: 'center',
  borderBottom: `2px solid ${theme.palette.divider}`,
  backgroundColor: theme.palette.background.default,
}))

const TitleContainer = styled(Box)(({ theme }) => ({
  display: 'flex',
  flexDirection: 'column',
  alignItems: 'center',
  justifyContent: 'center',
  marginBottom: theme.spacing(1),
  gap: theme.spacing(2),
}))

const Logo = styled('img')(({ theme }) => ({
  height: '120px',
  width: 'auto',
  [theme.breakpoints.down('sm')]: {
    height: '80px',
  },
}))

const Subtitle = styled(Typography)(({ theme }) => ({
  fontSize: '1rem',
  color: theme.palette.text.secondary,
  fontWeight: 400,
}))

const ComponentsGrid = styled(Box)(({ theme }) => ({
  display: 'flex',
  flexDirection: 'column',
  gap: theme.spacing(3),
}))

const SummaryError = styled(Alert)(({ theme }) => ({
  marginBottom: theme.spacing(3),
}))

const getLogoSrc = (isDarkMode: boolean, inOutage: boolean) => {
  if (inOutage) {
    return isDarkMode ? '/logo-outage-dark.svg' : '/logo-outage.svg'
  }
  return isDarkMode ? '/logo-dark.svg' : '/logo.svg'
}

const ComponentStatusList: React.FC = () => {
  const [components, setComponents] = useState<Component[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [inOutage, setInOutage] = useState(false)
  const {
    data: sloSummary,
    error: sloError,
    reload: reloadSLO,
  } = useAbortableGet<TeamSLOSummary>(getTeamSLOSummaryEndpoint())
  const [isDarkMode, setIsDarkMode] = useState(() => {
    const saved = localStorage.getItem('theme')
    return saved === 'dark'
  })

  useEffect(() => {
    // Listen for theme changes
    const handleThemeChange = () => {
      const saved = localStorage.getItem('theme')
      setIsDarkMode(saved === 'dark')
    }

    window.addEventListener('storage', handleThemeChange)
    window.addEventListener('themeChanged', handleThemeChange)
    return () => {
      window.removeEventListener('storage', handleThemeChange)
      window.removeEventListener('themeChanged', handleThemeChange)
    }
  }, [])

  const loadHomeStatus = useCallback((silent: boolean) => {
    Promise.all([
      fetch(getComponentsEndpoint()).then((res) => {
        if (!res.ok) throw new Error(`Failed to fetch components: HTTP ${res.status}`)
        return res.json()
      }),
      fetch(getOverallStatusEndpoint()).then((res) => {
        if (!res.ok) throw new Error(`Failed to fetch statuses: HTTP ${res.status}`)
        return res.json()
      }),
    ])
      .then(([componentsData, statusesData]) => {
        const statusMap = new Map<string, string>()
        statusesData.forEach((status: { component_name: string; status: string }) => {
          statusMap.set(status.component_name, status.status)
        })

        return componentsData.map((component: Component) => ({
          ...component,
          status: statusMap.get(component.name) || 'Unknown',
        }))
      })
      .then((data) => {
        setComponents(data)
        if (silent) {
          setError(null)
        }
      })
      .catch((err) => {
        if (!silent) {
          setError(err instanceof Error ? err.message : 'Failed to fetch components')
        }
      })
      .finally(() => {
        if (!silent) {
          setLoading(false)
        }
      })
  }, [])

  useEffect(() => {
    const cancel = deferMountFetch(() => {
      loadHomeStatus(false)
    })
    return () => {
      cancel()
    }
  }, [loadHomeStatus])

  useIntervalRefresh(() => {
    loadHomeStatus(true)
    reloadSLO(true)
  })

  if (loading) {
    return (
      <StyledContainer maxWidth="lg">
        <LoadingBox>
          <CircularProgress />
        </LoadingBox>
      </StyledContainer>
    )
  }

  if (error) {
    return (
      <StyledContainer maxWidth="lg">
        <Alert severity="error">{error}</Alert>
      </StyledContainer>
    )
  }

  return (
    <StyledContainer maxWidth="lg">
      <TitleSection data-tour="home-heading">
        <TitleContainer>
          <Logo src={getLogoSrc(isDarkMode, inOutage)} alt="SHIP Logo" />
        </TitleContainer>
        <Subtitle>Real-time monitoring of system components and availability</Subtitle>
      </TitleSection>

      <UnhealthyWell onHasOutagesChange={setInOutage} />

      {sloError && <SummaryError severity="error">{sloError}</SummaryError>}
      {sloSummary && sloSummary.teams.length > 0 && <TeamSLOSummaryWell summary={sloSummary} />}

      <ComponentsGrid data-tour="component-list">
        {components.map((component) => (
          <ComponentWell key={slugify(component.name)} component={component} />
        ))}
      </ComponentsGrid>
    </StyledContainer>
  )
}

export default ComponentStatusList
