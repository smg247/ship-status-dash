import type { SubComponentListParams } from '../types'

import { slugify } from './slugify'

export const getPublicDomain = () => {
  const envDomain = import.meta.env.VITE_PUBLIC_DOMAIN
  if (!envDomain) {
    throw new Error('VITE_PUBLIC_DOMAIN environment variable is required')
  }
  return envDomain
}

export const getProtectedDomain = () => {
  const envDomain = import.meta.env.VITE_PROTECTED_DOMAIN
  if (!envDomain) {
    throw new Error('VITE_PROTECTED_DOMAIN environment variable is required')
  }
  return envDomain
}

export const getComponentsEndpoint = () => `${getPublicDomain()}/api/components`

export const getTagsEndpoint = () => `${getPublicDomain()}/api/tags`

export const getComponentInfoEndpoint = (componentName: string) =>
  `${getPublicDomain()}/api/components/${slugify(componentName)}`

export const getOverallStatusEndpoint = () => `${getPublicDomain()}/api/status`

export const getSubComponentStatusEndpoint = (componentName: string, subComponentName: string) =>
  `${getPublicDomain()}/api/status/${slugify(componentName)}/${slugify(subComponentName)}`

export const getComponentStatusEndpoint = (componentName: string) =>
  `${getPublicDomain()}/api/status/${slugify(componentName)}`

export const getListSubComponentsEndpoint = (params: SubComponentListParams = {}) => {
  const search = new URLSearchParams()
  if (params.componentName) search.set('componentName', params.componentName)
  if (params.tag) search.set('tag', params.tag)
  if (params.team) search.set('team', params.team)
  if (params.status) {
    const statuses = Array.isArray(params.status) ? params.status : [params.status]
    for (const status of statuses) {
      search.append('status', status)
    }
  }
  const q = search.toString()
  return `${getPublicDomain()}/api/sub-components${q ? `?${q}` : ''}`
}

export const createOutageEndpoint = (componentName: string, subComponentName: string) =>
  `${getProtectedDomain()}/api/components/${slugify(componentName)}/${slugify(subComponentName)}/outages`

export const getSubComponentOutagesEndpoint = (componentName: string, subComponentName: string) =>
  `${getPublicDomain()}/api/components/${slugify(componentName)}/${slugify(subComponentName)}/outages`

export const modifyOutageEndpoint = (
  componentName: string,
  subComponentName: string,
  outageId: number,
) =>
  `${getProtectedDomain()}/api/components/${slugify(componentName)}/${slugify(subComponentName)}/outages/${outageId}`

export const getOutageEndpoint = (
  componentName: string,
  subComponentName: string,
  outageId: number,
) =>
  `${getPublicDomain()}/api/components/${slugify(componentName)}/${slugify(subComponentName)}/outages/${outageId}`

export const getOutageAuditLogsEndpoint = (
  componentName: string,
  subComponentName: string,
  outageId: number,
) =>
  `${getPublicDomain()}/api/components/${slugify(componentName)}/${slugify(subComponentName)}/outages/${outageId}/audit-logs`

export const getOutagesDuringEndpoint = (
  componentName: string,
  subComponentName?: string,
  start?: Date,
  end?: Date,
) => {
  const params = new URLSearchParams()
  params.set('componentName', slugify(componentName))
  if (subComponentName) params.set('subComponentName', slugify(subComponentName))
  if (start) params.set('start', start.toISOString())
  if (end) params.set('end', end.toISOString())
  return `${getPublicDomain()}/api/outages/during?${params.toString()}`
}

export const getSubComponentHistoryEndpoint = (
  componentName: string,
  subComponentName: string,
  days: number,
) =>
  `${getPublicDomain()}/api/components/${slugify(componentName)}/${slugify(subComponentName)}/outage-history?days=${days}`

export const getReportSuspectedOutageEndpoint = (componentName: string, subComponentName: string) =>
  `${getProtectedDomain()}/api/components/${slugify(componentName)}/${slugify(subComponentName)}/outages/report-suspected`

export const getTriageNotesEndpoint = (
  componentName: string,
  subComponentName: string,
  outageId: number,
) =>
  `${getProtectedDomain()}/api/components/${slugify(componentName)}/${slugify(subComponentName)}/outages/${outageId}/triage-notes`

export const getTriageNoteEndpoint = (
  componentName: string,
  subComponentName: string,
  outageId: number,
  noteId: number,
) =>
  `${getProtectedDomain()}/api/components/${slugify(componentName)}/${slugify(subComponentName)}/outages/${outageId}/triage-notes/${noteId}`

export const getOutageLinksEndpoint = (
  componentName: string,
  subComponentName: string,
  outageId: number,
) =>
  `${getProtectedDomain()}/api/components/${slugify(componentName)}/${slugify(subComponentName)}/outages/${outageId}/links`

export const getOutageLinkEndpoint = (
  componentName: string,
  subComponentName: string,
  outageId: number,
  linkId: number,
) =>
  `${getProtectedDomain()}/api/components/${slugify(componentName)}/${slugify(subComponentName)}/outages/${outageId}/links/${linkId}`

export const getOutageRelationshipsEndpoint = (
  componentName: string,
  subComponentName: string,
  outageId: number,
) =>
  `${getProtectedDomain()}/api/components/${slugify(componentName)}/${slugify(subComponentName)}/outages/${outageId}/relationships`

export const getOutageRelationshipEndpoint = (
  componentName: string,
  subComponentName: string,
  outageId: number,
  relationshipId: number,
) =>
  `${getProtectedDomain()}/api/components/${slugify(componentName)}/${slugify(subComponentName)}/outages/${outageId}/relationships/${relationshipId}`

export const getTeamSLOEndpoint = (team: string) =>
  `${getPublicDomain()}/api/teams/${encodeURIComponent(team)}/slo`

export const getTeamSLOSummaryEndpoint = () => `${getPublicDomain()}/api/teams/slo-summary`

export const putSLOItemEndpoint = (team: string) =>
  `${getProtectedDomain()}/api/teams/${encodeURIComponent(team)}/slo/items`

const sloItemLinkPath = (team: string, kind: string, itemKey: string) =>
  `${getProtectedDomain()}/api/teams/${encodeURIComponent(team)}/slo/items/${encodeURIComponent(kind)}/${encodeURIComponent(itemKey)}/links`

export const putSLOItemLinkEndpoint = (team: string, kind: string, itemKey: string) =>
  sloItemLinkPath(team, kind, itemKey)

export const deleteSLOItemLinkEndpoint = (
  team: string,
  kind: string,
  itemKey: string,
  linkId: number,
) => `${sloItemLinkPath(team, kind, itemKey)}/${linkId}`

export const getUserEndpoint = () => `${getProtectedDomain()}/api/user`

export const getExternalPageEndpoint = (slug: string) =>
  `${getPublicDomain()}/api/external-pages/${slug}`
