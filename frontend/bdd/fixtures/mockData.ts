import type {
  Component,
  ComponentStatus,
  Outage,
  OutageAuditLog,
  OutageDayBucket,
  SubComponent,
  SubComponentListItem,
  SuspectedOutageInfo,
  Tag,
  TriageNote,
  OutageLink,
  OutageRelationship,
  SLOEvaluation,
  SLOItem,
  TeamSLO,
  TeamSLOSummary,
} from '../../src/types'

interface MockAuthUser {
  username: string
  components: string[]
  team_slos?: string[]
}

const now = new Date().toISOString()
const oneHourAgo = new Date(Date.now() - 3600_000).toISOString()
const oneDayAgo = new Date(Date.now() - 86_400_000).toISOString()
const twoDaysAgo = new Date(Date.now() - 2 * 86_400_000).toISOString()

export const mockTags: Tag[] = [
  { name: 'ci', description: 'CI/CD related', color: '#1976d2' },
  { name: 'testing', description: 'Testing infrastructure', color: '#388e3c' },
]

const tideSubComponent: SubComponent = {
  name: 'Tide',
  slug: 'tide',
  description: 'Manages merge queue',
  requires_confirmation: false,
  tags: ['ci'],
  monitoring: { frequency: '5m', component_monitor: 'http', auto_resolve: true },
}

const deckSubComponent: SubComponent = {
  name: 'Deck',
  slug: 'deck',
  description: 'Prow dashboard UI',
  requires_confirmation: true,
  tags: ['ci'],
}

const hookSubComponent: SubComponent = {
  name: 'Hook',
  slug: 'hook',
  description: 'Webhook handler',
  requires_confirmation: false,
}

const registrySubComponent: SubComponent = {
  name: 'Registry',
  slug: 'registry',
  description: 'Container image registry',
  requires_confirmation: false,
  tags: ['testing'],
  monitoring: { frequency: '10m', component_monitor: 'http', auto_resolve: false },
}

const sippyUISubComponent: SubComponent = {
  name: 'Sippy UI',
  slug: 'sippy-ui',
  description: 'Test analytics dashboard',
  requires_confirmation: false,
}

export const incidentsSubComponent: SubComponent = {
  name: 'Incidents',
  slug: 'incidents',
  description: 'Currently open TRT incidents',
  requires_confirmation: false,
  exclude_from_main_outage_well: true,
}

export const mockComponents: Component[] = [
  {
    name: 'Prow',
    slug: 'prow',
    description: 'CI/CD system for Kubernetes',
    ship_team: 'TRT',
    sub_components: [tideSubComponent, deckSubComponent, hookSubComponent],
    owners: [{ rover_group: 'trt-team' }],
  },
  {
    name: 'Build Farm',
    slug: 'build-farm',
    description: 'Shared build infrastructure',
    ship_team: 'DPTP',
    sub_components: [registrySubComponent],
    owners: [{ rover_group: 'dptp-team' }],
  },
  {
    name: 'Sippy',
    slug: 'sippy',
    description: 'Test and job analysis platform',
    ship_team: 'TRT',
    sub_components: [sippyUISubComponent],
    owners: [{ rover_group: 'trt-team' }],
  },
]

export const mockComponentStatuses: ComponentStatus[] = [
  {
    component_name: 'Prow',
    status: 'Degraded',
    active_outages: [],
    sub_component_statuses: { Tide: 'Healthy', Deck: 'Degraded', Hook: 'Healthy' },
  },
  {
    component_name: 'Build Farm',
    status: 'Healthy',
    active_outages: [],
    sub_component_statuses: { Registry: 'Healthy' },
  },
  {
    component_name: 'Sippy',
    status: 'Down',
    active_outages: [],
    sub_component_statuses: { 'Sippy UI': 'Down' },
  },
]

export const mockActiveOutage: Outage = {
  ID: 1,
  CreatedAt: oneHourAgo,
  UpdatedAt: oneHourAgo,
  last_auditable_update: oneHourAgo,
  component_name: 'Prow',
  sub_component_name: 'Deck',
  severity: 'Degraded',
  start_time: oneHourAgo,
  end_time: { Time: '', Valid: false },
  auto_resolve: false,
  description: 'Deck UI is slow to respond',
  created_by: 'testuser',
  confirmed_at: { Time: '', Valid: false },
}

export const mockResolvedOutage: Outage = {
  ID: 2,
  CreatedAt: twoDaysAgo,
  UpdatedAt: oneDayAgo,
  last_auditable_update: oneDayAgo,
  component_name: 'Sippy',
  sub_component_name: 'Sippy UI',
  severity: 'Down',
  start_time: twoDaysAgo,
  end_time: { Time: oneDayAgo, Valid: true },
  auto_resolve: false,
  description: 'Sippy UI was completely unavailable',
  created_by: 'admin',
  resolved_by: 'admin',
  confirmed_by: 'admin',
  confirmed_at: { Time: twoDaysAgo, Valid: true },
  triage_notes: [
    {
      ID: 1,
      CreatedAt: twoDaysAgo,
      outage_id: 2,
      body: 'Investigating root cause',
      author: 'admin',
    },
  ],
  links: [
    {
      ID: 1,
      CreatedAt: twoDaysAgo,
      outage_id: 2,
      url: 'https://example.com/incident',
      link_type: 'incident_channel_thread',
      description: 'Slack thread',
    },
  ],
  relationships: [
    {
      ID: 30,
      CreatedAt: twoDaysAgo,
      outage_id: 2,
      related_outage_id: 1,
      relationship_type: 'caused_by',
      related_outage: {
        ID: 1,
        CreatedAt: oneHourAgo,
        UpdatedAt: oneHourAgo,
        last_auditable_update: oneHourAgo,
        component_name: 'prow',
        sub_component_name: 'deck',
        severity: 'Degraded',
        start_time: oneHourAgo,
        end_time: { Time: '', Valid: false },
        auto_resolve: false,
        description: 'Deck UI is slow to respond',
      },
    },
  ],
}

export const mockOutages: Outage[] = [mockActiveOutage, mockResolvedOutage]

export const mockOutageAuditLogs: OutageAuditLog[] = [
  {
    ID: 1,
    CreatedAt: twoDaysAgo,
    UpdatedAt: twoDaysAgo,
    outage_id: 2,
    user: 'admin',
    operation: 'create',
    new: JSON.stringify({ severity: 'Down', description: 'Sippy UI was completely unavailable' }),
  },
  {
    ID: 2,
    CreatedAt: oneDayAgo,
    UpdatedAt: oneDayAgo,
    outage_id: 2,
    user: 'admin',
    operation: 'update',
    old: JSON.stringify({ end_time: null }),
    new: JSON.stringify({ end_time: oneDayAgo }),
  },
]

export const mockTriageNote: TriageNote = {
  ID: 10,
  CreatedAt: now,
  outage_id: 1,
  body: 'New triage note',
  author: 'testuser',
}

export const mockOutageLink: OutageLink = {
  ID: 10,
  CreatedAt: now,
  outage_id: 1,
  url: 'https://example.com/rca',
  link_type: 'rca',
  description: 'Root cause analysis',
}

export const mockOutageRelationship: OutageRelationship = {
  ID: 20,
  CreatedAt: now,
  outage_id: 1,
  related_outage_id: 2,
  relationship_type: 'causes',
  related_outage: {
    ID: 2,
    CreatedAt: now,
    UpdatedAt: now,
    last_auditable_update: now,
    component_name: 'prow',
    sub_component_name: 'tide',
    severity: 'Down',
    start_time: oneDayAgo,
    end_time: { Time: now, Valid: true },
    auto_resolve: false,
    confirmed_at: { Time: now, Valid: true },
    description: 'Related outage',
  },
}

export const mockHistoryBuckets: OutageDayBucket[] = Array.from({ length: 14 }, (_, i) => {
  const date = new Date(Date.now() - (13 - i) * 86_400_000)
  return {
    date: date.toISOString().split('T')[0],
    highest_severity: i === 5 ? 'Degraded' : null,
    total_outage_minutes: i === 5 ? 45 : 0,
    outage_count: i === 5 ? 1 : 0,
  } as OutageDayBucket
})

export const mockUnhealthySubComponents: SubComponentListItem[] = [
  {
    ...deckSubComponent,
    component_name: 'Prow',
    status: 'Degraded',
    active_outages: [mockActiveOutage],
  },
  {
    ...sippyUISubComponent,
    component_name: 'Sippy',
    status: 'Down',
    active_outages: [],
  },
]

export const mockExcludedUnhealthySubComponent: SubComponentListItem = {
  ...incidentsSubComponent,
  component_name: 'TRT Incidents',
  status: 'Degraded',
  active_outages: [],
}

export const mockAuthUser: MockAuthUser = {
  username: 'testuser',
  components: ['prow', 'build-farm', 'sippy', 'Prow', 'Build Farm', 'Sippy'],
}

export const mockTRTSLOUser: MockAuthUser = {
  username: 'trt-owner',
  components: [],
  team_slos: ['TRT'],
}

const trtOccurredAt = '2026-09-25T12:00:00Z'

export const mockTRTPayloadItem: SLOItem = {
  id: 7,
  kind: 'payload_streams',
  schema_version: 1,
  item_key: 'nightly-rejected',
  group_key: '5.1.0-0.nightly',
  occurred_at: trtOccurredAt,
  outcome: 'Rejected',
  details: {
    payload_url: 'https://example.com/payload',
    jobs: [
      {
        name: 'e2e-job',
        url: 'https://example.com/payload/job',
        state: 'failure',
        recurring_count: 3,
      },
    ],
  },
  notes: '',
  updated_by: 'developer',
  links: [{ ID: 4, url: 'https://redhat.atlassian.net/browse/TRT-1', link_type: 'jira' }],
}

const trtEvaluation: SLOEvaluation = {
  name: 'accepted-payload-per-day',
  display_name: '1 accepted payload per day',
  source: 'payload_acceptance',
  met: false,
  result: {
    window: '24h',
    target: { min_accepted: 1 },
    groups: [{ key: '5.1.0-0.nightly', accepted: 0, met: false, last_accepted_at: trtOccurredAt }],
  },
}

const trtIncident: Outage = {
  ID: 42,
  CreatedAt: trtOccurredAt,
  UpdatedAt: trtOccurredAt,
  last_auditable_update: trtOccurredAt,
  component_name: 'trt-incidents',
  sub_component_name: 'incidents',
  severity: 'Degraded',
  start_time: trtOccurredAt,
  end_time: { Time: '', Valid: false },
  auto_resolve: false,
  description: 'CI payload rejected',
  confirmed_at: { Time: trtOccurredAt, Valid: true },
}

const trtSLOComponents = [
  {
    component: 'TRT Incidents',
    sub_component: 'Incidents',
    outages: [trtIncident],
  },
]

export const mockTRTTeamSLO: TeamSLO = {
  team: 'TRT',
  workspace: {
    kind: 'payload_streams',
    schema_version: 1,
    spec: {
      window: '24h',
      min_accepted: 1,
      recent_payloads: 2,
      streams: [{ release_controller: 'amd64', name: '5.1.0-0.nightly' }],
    },
  },
  evaluations: [trtEvaluation],
  slo_components: trtSLOComponents,
  items: [mockTRTPayloadItem],
}

export const mockTRTSummary: TeamSLOSummary = {
  teams: [
    {
      team: 'TRT',
      evaluations: [trtEvaluation],
      slo_components: trtSLOComponents,
    },
  ],
}

export const mockSuspectedOutage: SuspectedOutageInfo = {
  outage_id: 50,
  report_count: 3,
  description: 'Dashboard loading slowly',
  start_time: new Date(Date.now() - 600_000).toISOString(),
  reporters: ['user1', 'user2', 'user3'],
}
