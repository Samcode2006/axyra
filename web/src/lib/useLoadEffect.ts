import { useEffect } from 'react';

/**
 * Runs an async loader on mount and whenever the loader's identity changes
 * (wrap it in useCallback with its real dependencies). State is updated after
 * the awaited request, never synchronously during the effect.
 */
export function useLoadEffect(load: () => Promise<void>) {
  useEffect(() => {
    void load();
  }, [load]);
}
