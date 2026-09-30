import { Box, styled, Typography } from '@mui/material'

const Section = styled(Box)(({ theme }) => ({
  backgroundColor: theme.palette.background.paper,
  borderRadius: theme.spacing(2),
  padding: theme.spacing(3),
  marginBottom: theme.spacing(3),
  border: `1px solid ${theme.palette.divider}`,
}))

interface UnknownSLOWorkspaceProps {
  kind: string
  schemaVersion: number
  itemCount: number
}

const UnknownSLOWorkspace = ({ kind, schemaVersion, itemCount }: UnknownSLOWorkspaceProps) => (
  <Section>
    <Typography variant="h6">Unsupported SLO workspace</Typography>
    <Typography color="text.secondary">
      {kind} version {schemaVersion} ({itemCount} items) has no renderer in this build.
    </Typography>
  </Section>
)

export default UnknownSLOWorkspace
