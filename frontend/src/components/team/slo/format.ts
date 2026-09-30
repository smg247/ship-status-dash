import { relativeTime } from '../../../utils/helpers'
import { slugify } from '../../../utils/slugify'

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

export const sloComponentDomId = (componentName: string, subComponentName: string) =>
  `slo-component-${slugify(componentName)}-${slugify(subComponentName)}`

export const streamDomId = (streamName: string) => `stream-${streamName}`

const jiraKeyPattern = /\/browse\/([A-Z][A-Z0-9]+-\d+)\/?(?:[?#].*)?$/
const outageLinkPattern = /^\/([^/]+)\/([^/]+)\/outages\/\d+/

export const linkLabel = (link: { link_type: string; url: string }): string => {
  if (link.link_type === 'jira') {
    const match = link.url.match(jiraKeyPattern)
    if (match) {
      return match[1]
    }
  }
  if (link.link_type === 'outage') {
    const match = link.url.match(outageLinkPattern)
    if (match) {
      return `${match[1]}/${match[2]}`
    }
  }
  return link.link_type
}

export const outagePath = (outage: {
  component_name: string
  sub_component_name: string
  ID: number
}) => `/${outage.component_name}/${outage.sub_component_name}/outages/${outage.ID}`

export const openedLabel = (start: string) => `Opened ${relativeTime(new Date(start), new Date())}`
