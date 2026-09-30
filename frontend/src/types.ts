export type Status =
  | 'Healthy'
  | 'Degraded'
  | 'Down'
  | 'Suspected'
  | 'Partial'
  | 'Unknown'
  | 'CapacityExhausted'

export interface SuspectedOutageInfo {
  outage_id: number
  report_count: number
  description?: string
  start_time: string
  reporters: string[]
}

export interface ComponentStatus {
  component_name: string
  status: Status
  active_outages: Outage[]
  last_ping_time?: string
  sub_component_statuses?: Record<string, Status>
  suspected_outage?: SuspectedOutageInfo
}

export interface OutageDayBucket {
  date: string // YYYY-MM-DD
  highest_severity: Status | null
  total_outage_minutes: number
  outage_count: number
}

export interface Reason {
  ID: number
  CreatedAt: string
  UpdatedAt: string
  type: string
  check: string
  results: string
}

export interface SlackThread {
  channel: string
  thread_url: string
}

export interface TriageNote {
  ID: number
  CreatedAt: string
  outage_id: number
  body: string
  author: string
}

export interface OutageLink {
  ID: number
  CreatedAt: string
  outage_id: number
  url: string
  link_type: 'incident_channel_thread' | 'rca' | 'jira' | 'other'
  description?: string
}

export interface OutageRelationship {
  ID: number
  CreatedAt: string
  outage_id: number
  related_outage_id: number
  relationship_type: 'causes' | 'caused_by' | 'related_to'
  related_outage?: Outage
}

export interface OutageAuditLog {
  ID: number
  CreatedAt: string
  UpdatedAt: string
  outage_id: number
  user: string
  operation: string
  old?: string
  new?: string
}

export interface Outage {
  ID: number
  CreatedAt: string
  UpdatedAt: string
  last_auditable_update: string
  component_name: string
  sub_component_name: string
  severity: string
  start_time: string
  end_time: {
    Time: string
    Valid: boolean
  }
  auto_resolve: boolean
  description?: string
  discovered_from?: string
  created_by?: string
  resolved_by?: string
  confirmed_by?: string
  confirmed_at: {
    Time: string
    Valid: boolean
  }
  triage_notes?: TriageNote[]
  links?: OutageLink[]
  relationships?: OutageRelationship[]
  reasons?: Reason[]
  slack_threads?: SlackThread[]
}

export interface Monitoring {
  frequency: string
  component_monitor: string
  auto_resolve: boolean
  outage_per_reason?: boolean
}

export interface SlackReportingConfig {
  channel: string
  severity?: string
}

export interface SubComponent {
  name: string
  slug: string
  description: string
  long_description?: string
  documentation_url?: string
  tags?: string[]
  requires_confirmation: boolean
  critical?: boolean
  exclude_from_main_outage_well?: boolean
  monitoring?: Monitoring
  slack_reporting?: SlackReportingConfig[]
  status?: Status
  active_outages?: Outage[]
}

export interface SubComponentListItem extends SubComponent {
  component_name: string
}

export interface SubComponentListParams {
  componentName?: string
  tag?: string
  team?: string
  /** One or more statuses; when set, only matching sub-components are returned. Status is always included on each item. */
  status?: Status | Status[]
}

export interface Component {
  name: string
  slug: string
  description: string
  ship_team: string
  slack_reporting?: SlackReportingConfig[]
  sub_components: SubComponent[]
  owners: Array<{
    rover_group?: string
    service_account?: string
    user?: string
  }>
  slo_component?: boolean
  status?: string
  last_ping_time?: string
}

export interface SLOWorkspace {
  kind: string
  schema_version: number
  spec?: unknown
}

export interface SLOEvaluation {
  name: string
  display_name: string
  source: string
  met: boolean
  result?: unknown
}

export interface SLOJob {
  name: string
  url: string
  state: string
  notes?: string
  recurring_count?: number
}

export interface SLOPayloadDetails {
  payload_url?: string
  analysis_url?: string
  jobs: SLOJob[]
}

export interface SLOItemLink {
  ID: number
  url: string
  link_type: 'jira' | 'outage' | 'other'
  outage_id?: number
}

export interface SLOItem {
  id: number
  kind: string
  schema_version: number
  item_key: string
  group_key: string
  occurred_at: string
  outcome: string
  details: SLOPayloadDetails
  notes: string
  updated_by: string
  links: SLOItemLink[]
}

export interface SLOComponentBlock {
  component: string
  sub_component: string
  outages: Outage[]
}

export interface TeamSLO {
  team: string
  workspace?: SLOWorkspace
  evaluations: SLOEvaluation[]
  slo_components: SLOComponentBlock[]
  items: SLOItem[]
}

export interface TeamSLOSummary {
  teams: Array<{
    team: string
    evaluations: SLOEvaluation[]
    slo_components: SLOComponentBlock[]
  }>
}

export interface Tag {
  name: string
  description: string
  color: string
}
