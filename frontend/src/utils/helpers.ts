import type { Theme } from '@mui/material/styles'

import type { Outage } from '../types'

const STATUS_RANK = [
  'Healthy',
  'Unknown',
  'Partial',
  'Suspected',
  'Degraded',
  'CapacityExhausted',
  'Down',
]

export const outageStatus = (outage: Outage): string => {
  if (outage.end_time?.Valid) {
    return 'Healthy'
  }
  if (!outage.confirmed_at?.Valid) {
    return 'Suspected'
  }
  return outage.severity
}

export const worstOutageStatus = (outages: Outage[]): string | undefined => {
  if (outages.length === 0) {
    return undefined
  }
  return outages.reduce((worst, outage) => {
    const status = outageStatus(outage)
    return STATUS_RANK.indexOf(status) > STATUS_RANK.indexOf(worst) ? status : worst
  }, 'Healthy')
}

const getStatusKey = (status: string): keyof Theme['palette']['status'] | null => {
  switch (status) {
    case 'Healthy':
      return 'healthy'
    case 'Degraded':
      return 'degraded'
    case 'Down':
      return 'down'
    case 'CapacityExhausted':
      return 'capacityExhausted'
    case 'Suspected':
      return 'suspected'
    case 'Partial':
      return 'partial'
    case 'Unknown':
      return 'unknown'
    default:
      return null
  }
}

export const getStatusBackgroundColor = (theme: Theme, status: string) => {
  const statusKey = getStatusKey(status)
  if (statusKey) {
    return theme.palette.status[statusKey].background
  }
  return theme.palette.mode === 'dark' ? theme.palette.grey[800] : theme.palette.grey[100]
}

export const getStatusChipColor = (theme: Theme, status: string) => {
  const statusKey = getStatusKey(status)
  if (statusKey) {
    return theme.palette.status[statusKey].main
  }
  return theme.palette.mode === 'dark' ? theme.palette.grey[300] : theme.palette.grey[500]
}

export const getSeverityColor = (theme: Theme, severity: string) => {
  const statusKey = getStatusKey(severity)
  if (statusKey) {
    return theme.palette.status[statusKey].main
  }
  return theme.palette.info.main
}

// Helper function to format status or severity for display
export const formatStatusSeverityText = (text: string): string => {
  switch (text) {
    case 'CapacityExhausted':
      return 'Capacity Exhausted'
    default:
      return text
  }
}

// Helper function to format dates to second precision
export const formatDateToSeconds = (dateString: string) => {
  if (!dateString) return ''
  const date = new Date(dateString)
  return date.toISOString().replace(/\.\d{3}Z$/, 'Z')
}

// relativeTime shows a plain English rendering of a time, e.g. "30 minutes ago".
// This is because the ES6 Intl.RelativeTime isn't available in all environments yet,
// e.g. Safari and NodeJS.
export const relativeTime = (date: Date, startDate: Date) => {
  const minute = 1000 * 60 // Milliseconds in a minute
  const hour = 60 * minute // Milliseconds in an hour
  const day = 24 * hour // Milliseconds in a day

  const millisAgo = date.getTime() - startDate.getTime()
  if (Math.abs(millisAgo) < hour) {
    return Math.round(Math.abs(millisAgo) / minute) + ' minutes ago'
  } else if (Math.abs(millisAgo) < day) {
    const hours = Math.round(Math.abs(millisAgo) / hour)
    return `${hours} ${hours === 1 ? 'hour' : 'hours'} ago`
  } else if (Math.abs(millisAgo) < 1.5 * day) {
    return 'about a day ago'
  } else {
    return Math.round(Math.abs(millisAgo) / day) + ' days ago'
  }
}

// relativeDuration shows a plain English rendering of a duration, e.g. "30 minutes".
export const relativeDuration = (secondsAgo: number) => {
  if (secondsAgo === undefined) {
    return { value: 'N/A', units: 'N/A' }
  }

  const minute = 60
  const hour = 60 * minute
  const day = 24 * hour

  if (Math.abs(secondsAgo) < hour) {
    return { value: Math.abs(secondsAgo) / minute, units: 'minutes' }
  } else if (Math.abs(secondsAgo) < day) {
    const hours = Math.abs(secondsAgo) / hour
    return { value: hours, units: hours === 1 ? 'hour' : 'hours' }
  } else if (Math.abs(secondsAgo) < 1.5 * day) {
    return { value: 1, units: 'day' }
  } else {
    const days = Math.abs(secondsAgo) / day
    return { value: days, units: days === 1 ? 'day' : 'days' }
  }
}

// formatDuration formats a duration between start time and optional end time as a human-readable string
export const formatDuration = (
  startTime: string,
  endTime?: { Time: string; Valid: boolean },
): string => {
  const start = new Date(startTime)
  const end = endTime?.Valid ? new Date(endTime.Time) : new Date()
  const durationSeconds = Math.floor((end.getTime() - start.getTime()) / 1000)
  const duration = relativeDuration(durationSeconds)
  return `${Math.round(Number(duration.value))} ${duration.units}`
}

// formatPreciseDuration formats an outage duration as an exact h/m/s string.
// Returns "Ongoing" if the end time is not set.
export const formatPreciseDuration = (
  startStr: string,
  endTime: { Time: string; Valid: boolean },
): string => {
  if (!endTime.Valid) return 'Ongoing'
  const ms = new Date(endTime.Time).getTime() - new Date(startStr).getTime()
  const hours = Math.floor(ms / 3600000)
  const mins = Math.floor((ms % 3600000) / 60000)
  const secs = Math.floor((ms % 60000) / 1000)
  if (hours > 0) return `${hours}h ${mins}m`
  if (mins > 0) return `${mins}m`
  return `${secs}s`
}

// formatMinutes converts a duration in minutes to an exact h/m/s string.
export const formatMinutes = (minutes: number): string => {
  const hours = Math.floor(minutes / 60)
  const mins = Math.floor(minutes % 60)
  const secs = Math.floor((minutes % 1) * 60)
  if (hours > 0) return `${hours}h ${mins}m`
  if (mins > 0) return `${mins}m`
  return `${secs}s`
}

// Helper function to get current local time in datetime-local format
export const getCurrentLocalTime = () => {
  return formatDateForDateTimeLocal(new Date())
}

// Helper function to format a date to datetime-local format (YYYY-MM-DDTHH:mm)
// This preserves the local timezone instead of converting to UTC
export const formatDateForDateTimeLocal = (date: Date) => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  return `${year}-${month}-${day}T${hours}:${minutes}`
}
