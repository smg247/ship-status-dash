import { useCallback, useEffect, useRef, useState } from 'react'

import { deferMountFetch } from '../utils/deferMountFetch'

interface UseAbortableGetResult<T> {
  data: T | null
  loading: boolean
  error: string | null
  reload: (silent?: boolean) => void
}

const isAbortError = (err: unknown) => err instanceof DOMException && err.name === 'AbortError'

const useAbortableGet = <T>(url: string | null): UseAbortableGetResult<T> => {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(url !== null)
  const [error, setError] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const reload = useCallback(
    (silent = false) => {
      abortRef.current?.abort()
      if (!url) {
        setData(null)
        setError(null)
        setLoading(false)
        return
      }

      const controller = new AbortController()
      abortRef.current = controller

      if (!silent) {
        setData(null)
        setError(null)
        setLoading(true)
      }

      fetch(url, { signal: controller.signal })
        .then((response) => {
          if (!response.ok) {
            throw new Error(`Failed to load SLO data (${response.status})`)
          }
          return response.json() as Promise<T>
        })
        .then((body) => {
          if (controller.signal.aborted) {
            return
          }
          setData(body)
          setError(null)
        })
        .catch((err: unknown) => {
          if (isAbortError(err) || controller.signal.aborted) {
            return
          }
          setError(err instanceof Error ? err.message : 'Failed to load SLO data')
        })
        .finally(() => {
          if (!controller.signal.aborted) {
            setLoading(false)
          }
        })
    },
    [url],
  )

  useEffect(() => {
    const cancel = deferMountFetch(() => {
      reload(false)
    })
    return () => {
      cancel()
      abortRef.current?.abort()
    }
  }, [reload])

  return { data, loading, error, reload }
}

export default useAbortableGet
