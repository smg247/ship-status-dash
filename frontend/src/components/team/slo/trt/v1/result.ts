import type { SLOEvaluation, SLOWorkspace } from '../../../../../types'

export interface TRTPayloadStream {
  release_controller: string
  name: string
}

export interface TRTPayloadSettings {
  window: string
  min_accepted: number
  recent_payloads: number
  streams: TRTPayloadStream[]
}

export interface TRTPayloadGroup {
  key: string
  accepted: number
  met: boolean
  last_accepted_at?: string
}

export interface TRTPayloadResult {
  window: string
  target: { min_accepted: number }
  groups: TRTPayloadGroup[]
}

export const trtPayloadSettings = (workspace: SLOWorkspace): TRTPayloadSettings | undefined => {
  if (workspace.kind !== 'payload_streams' || workspace.schema_version !== 1 || !workspace.spec) {
    return undefined
  }
  return workspace.spec as TRTPayloadSettings
}

export const trtPayloadResult = (evaluation?: SLOEvaluation): TRTPayloadResult | undefined => {
  if (!evaluation || evaluation.source !== 'payload_acceptance' || !evaluation.result) {
    return undefined
  }
  return evaluation.result as TRTPayloadResult
}

export const worstMiss = (result: TRTPayloadResult): TRTPayloadGroup | undefined => {
  const missed = result.groups.filter((group) => !group.met)
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
