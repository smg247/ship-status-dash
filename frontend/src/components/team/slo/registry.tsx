import type { ReactNode } from 'react'

import type { PayloadStreamsWorkspaceProps } from './trt/v1/PayloadStreamsWorkspace'
import PayloadStreamsWorkspace from './trt/v1/PayloadStreamsWorkspace'
import UnknownSLOWorkspace from './unknown/UnknownSLOWorkspace'

export const renderTeamWorkspace = (props: PayloadStreamsWorkspaceProps): ReactNode => {
  const { team, workspace, items } = props
  if (team === 'TRT' && workspace.kind === 'payload_streams' && workspace.schema_version === 1) {
    return <PayloadStreamsWorkspace {...props} />
  }
  return (
    <UnknownSLOWorkspace
      kind={workspace.kind}
      schemaVersion={workspace.schema_version}
      itemCount={items.length}
    />
  )
}
