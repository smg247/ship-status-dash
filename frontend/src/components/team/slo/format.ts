import type { Outage, SLOEvaluation, SLOGroupEval } from '../../../types'

export const formatAge = (iso?: string): string => {
  if (!iso) {
    return 'none'
  }
  const hours = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 36e5))
  if (hours < 48) {
    return `${hours}h`
  }
  return `${Math.round(hours / 24)}d`
}

export const worstMiss = (evaluation: SLOEvaluation): SLOGroupEval | undefined => {
  const missed = evaluation.groups.filter((group) => !group.met)
  if (missed.length === 0) {
    return undefined
  }
  return missed.reduce((worst, group) => {
    if (!worst.last_accepted_at) {
      return worst
    }
    if (!group.last_accepted_at) {
      return group
    }
    return new Date(group.last_accepted_at) < new Date(worst.last_accepted_at) ? group : worst
  })
}

export const sloComponentDomId = (componentName: string) =>
  `slo-component-${componentName
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '')}`

export const streamDomId = (streamName: string) => `stream-${streamName}`

const jiraKeyPattern = /\/browse\/([A-Z][A-Z0-9]+-\d+)\/?(?:[?#].*)?$/

export const linkLabel = (link: { link_type: string; url: string }): string => {
  if (link.link_type === 'jira') {
    const match = link.url.match(jiraKeyPattern)
    if (match) {
      return match[1]
    }
  }
  if (link.link_type === 'outage') {
    return 'Outage'
  }
  return link.link_type
}

export const outagePath = (outage: Outage) =>
  `/${outage.component_name}/${outage.sub_component_name}/outages/${outage.ID}`

export const openedLabel = (start: string) => {
  const hours = Math.max(0, Math.round((Date.now() - new Date(start).getTime()) / 36e5))
  if (hours < 48) {
    return `Opened ${hours}h ago`
  }
  return `Opened ${Math.round(hours / 24)}d ago`
}

const SEVERITY_RANK = [
  'Healthy',
  'Unknown',
  'Partial',
  'Suspected',
  'Degraded',
  'CapacityExhausted',
  'Down',
]

export const outageTintStatus = (outage: Outage): string => {
  if (outage.end_time?.Valid) {
    return 'Healthy'
  }
  if (!outage.confirmed_at?.Valid) {
    return 'Suspected'
  }
  return outage.severity
}

export const worstOutageTint = (outages: Outage[]): string | undefined => {
  if (outages.length === 0) {
    return undefined
  }
  return outages.reduce((worst, outage) => {
    const tint = outageTintStatus(outage)
    return SEVERITY_RANK.indexOf(tint) > SEVERITY_RANK.indexOf(worst) ? tint : worst
  }, 'Healthy')
}
