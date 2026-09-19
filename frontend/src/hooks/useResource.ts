import { useCallback, useEffect, useState } from 'react'

export function useResource<T>(loader: () => Promise<T>, dependencies: unknown[] = []) {
  const [data, setData] = useState<T>()
  const [error, setError] = useState<Error>()
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    setLoading(true)
    setError(undefined)
    try {
      setData(await loader())
    } catch (reason) {
      setError(reason instanceof Error ? reason : new Error(String(reason)))
    } finally {
      setLoading(false)
    }
  }, dependencies)

  useEffect(() => { void refresh() }, [refresh])
  return { data, error, loading, refresh }
}
