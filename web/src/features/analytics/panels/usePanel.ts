import { useCallback, useEffect, useRef, useState } from 'react';
import { errMsg } from '../../../lib/api';

interface Settled<T> {
  /** The request this result belongs to. */
  key: string;
  data: T | null;
  error: string | null;
}

export interface PanelState<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
  reload: () => void;
}

/**
 * Loads one panel's data independently of the others, so a failure in one query
 * shows an error in that panel only.
 *
 * requestKey identifies the request: the data is reloaded whenever it changes.
 * Build it from everything the fetcher depends on (range, tenant, options).
 *
 * "loading" is derived, not stored: a result only counts for the request key it
 * was fetched under, so changing the key reads as loading immediately and a late
 * response from a superseded request is ignored.
 */
export function usePanel<T>(fetcher: () => Promise<T>, requestKey: string): PanelState<T> {
  const [settled, setSettled] = useState<Settled<T> | null>(null);
  // Bumped by reload() to refetch under the same key.
  const [attempt, setAttempt] = useState(0);
  const fetchRef = useRef(fetcher);
  useEffect(() => {
    fetchRef.current = fetcher;
  });

  const fullKey = `${requestKey}#${attempt}`;

  useEffect(() => {
    let current = true;
    fetchRef
      .current()
      .then((data) => {
        if (current) setSettled({ key: fullKey, data, error: null });
      })
      .catch((err) => {
        if (current) setSettled({ key: fullKey, data: null, error: errMsg(err, 'Failed to load') });
      });
    return () => {
      current = false; // a newer request has started, or the panel unmounted
    };
  }, [fullKey]);

  const reload = useCallback(() => setAttempt((a) => a + 1), []);

  const fresh = settled !== null && settled.key === fullKey;
  return {
    data: fresh ? settled.data : null,
    error: fresh ? settled.error : null,
    loading: !fresh,
    reload,
  };
}
