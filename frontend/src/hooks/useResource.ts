import { useCallback, useEffect, useState } from 'react'

export function useResource<T>(loader: (fresh?: boolean) => Promise<T>, dependencies: unknown[] = []) {
  const [data, setData] = useState<T>()
  const [error, setError] = useState<Error>()
  const [loading, setLoading] = useState(true)

  const load = useCallback(async (fresh: boolean) => {
    setLoading(true)
    setError(undefined)
    try {
      setData(await loader(fresh))
    } catch (reason) {
      setError(reason instanceof Error ? reason : new Error(String(reason)))
    } finally {
      setLoading(false)
    }
  }, dependencies)

  const refresh = useCallback(() => load(true), [load])
  useEffect(() => { void load(false) }, [load])
  return { data, error, loading, refresh }
}
