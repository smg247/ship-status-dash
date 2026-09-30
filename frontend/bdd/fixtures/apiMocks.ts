import type { Page, Route } from '@playwright/test'

import {
  mockAuthUser,
  mockComponents,
  mockComponentStatuses,
  mockHistoryBuckets,
  mockOutageAuditLogs,
  mockOutageLink,
  mockOutageRelationship,
  mockOutages,
  mockTags,
  mockTriageNote,
  mockUnhealthySubComponents,
} from './mockData'

export const PUBLIC = 'http://localhost:8180'
export const PROTECTED = 'http://localhost:3030'

interface MockApiOptions {
  authenticated?: boolean
  unhealthySubComponents?: typeof mockUnhealthySubComponents
}

function toSlug(name: string): string {
  return name.toLowerCase().replace(/[^a-z0-9]+/g, '-')
}

export function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

export async function setupApiMocks(page: Page, options: MockApiOptions = {}) {
  const { authenticated = false, unhealthySubComponents = mockUnhealthySubComponents } = options

  // Dismiss the app tour overlay so it doesn't intercept clicks
  await page.addInitScript(() => {
    const allRouteTypes = [
      'home',
      'subcomponent-detail',
      'outage-detail',
      'external-page',
      'status-history',
    ]
    localStorage.setItem('shipStatusTourSeenRouteTypes', JSON.stringify(allRouteTypes))
  })

  // --- Auth ---
  await page.route(`${PROTECTED}/api/user`, (route) => {
    if (authenticated) {
      return json(route, mockAuthUser)
    }
    return json(route, { error: 'unauthorized' }, 401)
  })

  // --- Components ---
  await page.route(`${PUBLIC}/api/components`, async (route) => {
    const url = new URL(route.request().url())
    const pathAfterComponents = url.pathname.replace('/api/components', '')

    if (!pathAfterComponents || pathAfterComponents === '/') {
      return json(route, mockComponents)
    }
    return route.fallback()
  })

  // --- Component info (/:slug) ---
  await page.route(`${PUBLIC}/api/components/*`, async (route) => {
    const url = new URL(route.request().url())
    const segments = url.pathname.split('/').filter(Boolean)

    if (segments.length === 3) {
      const slug = segments[2]
      const component = mockComponents.find((c) => c.slug === slug)
      if (component) {
        return json(route, component)
      }
      return json(route, { error: 'not found' }, 404)
    }
    return route.fallback()
  })

  // --- Overall status ---
  await page.route(`${PUBLIC}/api/status`, (route) => {
    const url = new URL(route.request().url())
    if (url.pathname === '/api/status') {
      return json(route, mockComponentStatuses)
    }
    return route.fallback()
  })

  // --- Component status (/:slug) ---
  await page.route(`${PUBLIC}/api/status/*`, (route) => {
    const url = new URL(route.request().url())
    const segments = url.pathname.split('/').filter(Boolean)

    if (segments.length === 3) {
      const slug = segments[2]
      const status = mockComponentStatuses.find((s) => toSlug(s.component_name) === slug)
      if (status) {
        return json(route, status)
      }
    }
    return route.fallback()
  })

  // --- Sub-component status (/:comp/:sub) ---
  await page.route(`${PUBLIC}/api/status/*/*`, (route) => {
    const url = new URL(route.request().url())
    const segments = url.pathname.split('/').filter(Boolean)
    if (segments.length === 4) {
      const compSlug = segments[2]
      const subSlug = segments[3]
      const compStatus = mockComponentStatuses.find((s) => toSlug(s.component_name) === compSlug)
      let subStatus = 'Unknown'
      if (compStatus && compStatus.sub_component_statuses) {
        const matchedKey = Object.keys(compStatus.sub_component_statuses).find(
          (k) => toSlug(k) === subSlug,
        )
        if (matchedKey) {
          subStatus = compStatus.sub_component_statuses[matchedKey] || 'Unknown'
        }
      }

      return json(route, {
        component_name: compStatus ? compStatus.component_name : compSlug,
        status: subStatus,
        active_outages: mockOutages.filter(
          (o) =>
            toSlug(o.component_name) === compSlug &&
            toSlug(o.sub_component_name) === subSlug &&
            !o.end_time.Valid,
        ),
        last_ping_time: new Date().toISOString(),
      })
    }
    return route.fallback()
  })

  // --- Sub-components list ---
  await page.route(`${PUBLIC}/api/sub-components**`, (route) => {
    const url = new URL(route.request().url())
    const statusFilters = url.searchParams.getAll('status')
    const tag = url.searchParams.get('tag')
    const team = url.searchParams.get('team')

    let items = unhealthySubComponents
    if (statusFilters.length === 0 && !tag && !team) {
      items = mockComponents.flatMap((c) =>
        c.sub_components.map((sc) => ({
          ...sc,
          component_name: c.name,
          status:
            mockComponentStatuses.find((s) => s.component_name === c.name)
              ?.sub_component_statuses?.[sc.name] ?? 'Unknown',
        })),
      )
    }
    if (tag) {
      items = items.filter((i) => i.tags?.includes(tag))
    }
    if (team) {
      const teamComponents = mockComponents.filter((c) => c.ship_team === team)
      items = items.filter((i) => teamComponents.some((c) => c.name === i.component_name))
    }
    return json(route, items)
  })

  // --- Outages ---
  await page.route(`${PUBLIC}/api/components/*/*/outages`, (route) => {
    const url = new URL(route.request().url())
    const segments = url.pathname.split('/').filter(Boolean)
    const compSlug = segments[2]
    const subSlug = segments[3]

    const filtered = mockOutages.filter(
      (o) => toSlug(o.component_name) === compSlug && toSlug(o.sub_component_name) === subSlug,
    )
    return json(route, filtered)
  })

  // --- Outages during ---
  await page.route(`${PUBLIC}/api/outages/during**`, (route) => {
    return json(route, mockOutages)
  })

  // --- Single outage ---
  await page.route(`${PUBLIC}/api/components/*/*/outages/*`, (route) => {
    const url = new URL(route.request().url())
    if (url.pathname.includes('audit-logs')) {
      return route.fallback()
    }
    if (url.pathname.includes('outage-history')) {
      return route.fallback()
    }

    const segments = url.pathname.split('/').filter(Boolean)
    const outageId = parseInt(segments[5], 10)
    const outage = mockOutages.find((o) => o.ID === outageId)
    if (outage) {
      return json(route, outage)
    }
    return json(route, { error: 'not found' }, 404)
  })

  // --- Audit logs ---
  await page.route(`${PUBLIC}/api/components/*/*/outages/*/audit-logs`, (route) => {
    return json(route, mockOutageAuditLogs)
  })

  // --- Outage history ---
  await page.route(`${PUBLIC}/api/components/*/*/outage-history**`, (route) => {
    return json(route, mockHistoryBuckets)
  })

  // --- Tags ---
  await page.route(`${PUBLIC}/api/teams/slo-summary`, (route) => json(route, { teams: [] }))
  await page.route(`${PUBLIC}/api/teams/*/slo`, (route) =>
    json(route, { team: '', evaluations: [], slo_components: [], items: [] }),
  )

  await page.route(`${PUBLIC}/api/tags`, (route) => {
    return json(route, mockTags)
  })

  // --- Mutations (protected) ---
  await page.route(`${PROTECTED}/api/components/*/*/outages`, (route) => {
    if (route.request().method() === 'POST') {
      return json(route, { ...mockOutages[0], ID: 99 }, 201)
    }
    return route.fallback()
  })

  await page.route(`${PROTECTED}/api/components/*/*/outages/*`, (route) => {
    const method = route.request().method()
    if (method === 'PATCH') {
      return json(route, mockOutages[0])
    }
    if (method === 'DELETE') {
      return route.fulfill({ status: 204 })
    }
    return route.fallback()
  })

  await page.route(`${PROTECTED}/api/components/*/*/outages/report-suspected`, (route) => {
    return json(route, { outage_id: 99, report_count: 1 })
  })

  // --- Triage notes ---
  await page.route(`${PROTECTED}/api/components/*/*/outages/*/triage-notes`, (route) => {
    if (route.request().method() === 'POST') {
      return json(route, mockTriageNote, 201)
    }
    return route.fallback()
  })

  await page.route(`${PROTECTED}/api/components/*/*/outages/*/triage-notes/*`, (route) => {
    const method = route.request().method()
    if (method === 'PATCH') {
      return json(route, { ...mockTriageNote, body: 'Updated note' })
    }
    if (method === 'DELETE') {
      return route.fulfill({ status: 204 })
    }
    return route.fallback()
  })

  // --- Outage links ---
  await page.route(`${PROTECTED}/api/components/*/*/outages/*/links`, (route) => {
    if (route.request().method() === 'POST') {
      return json(route, mockOutageLink, 201)
    }
    return route.fallback()
  })

  await page.route(`${PROTECTED}/api/components/*/*/outages/*/links/*`, (route) => {
    const method = route.request().method()
    if (method === 'PATCH') {
      return json(route, { ...mockOutageLink, description: 'Updated link' })
    }
    if (method === 'DELETE') {
      return route.fulfill({ status: 204 })
    }
    return route.fallback()
  })

  // --- Outage relationships ---
  await page.route(`${PROTECTED}/api/components/*/*/outages/*/relationships`, (route) => {
    if (route.request().method() === 'POST') {
      return json(route, mockOutageRelationship, 201)
    }
    return route.fallback()
  })

  await page.route(`${PROTECTED}/api/components/*/*/outages/*/relationships/*`, (route) => {
    if (route.request().method() === 'DELETE') {
      return route.fulfill({ status: 204 })
    }
    return route.fallback()
  })
}
