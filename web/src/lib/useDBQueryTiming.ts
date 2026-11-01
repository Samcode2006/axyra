import { useEffect, useState } from 'react';
import type { AxiosResponse, InternalAxiosRequestConfig } from 'axios';
import { http } from './api';

export interface DBQueryTiming {
  ms: number | null;
  orders: number;
  sequence: number;
}

/** Read Go's measured aggregate-query duration; never substitute browser timing. */
export function useDBQueryTiming(scopeKey: string, kind: 'summary' | 'revenue' = 'summary') {
  const [snapshot, setSnapshot] = useState<{ scopeKey: string; rows: Record<string, DBQueryTiming> }>();

  useEffect(() => {
    let active = true;
    let sequence = 0;
    const requests = new WeakMap<InternalAxiosRequestConfig, number>();
    const request = http.interceptors.request.use((config) => {
      if (config.method === 'get' && config.url === `/v1/analytics/orders/${kind}`) {
        requests.set(config, ++sequence);
      }
      return config;
    });
    const finish = (config: InternalAxiosRequestConfig | undefined, response?: AxiosResponse) => {
      if (!active || !config) return;
      const started = requests.get(config);
      if (started == null) return;
      requests.delete(config);
      const raw = response?.headers['x-db-query-ms'];
      const ms = raw == null || String(raw).trim() === '' ? NaN : Number(raw);
      const orders = Number(response?.data?.[kind === 'summary' ? 'total_orders' : 'active_orders']);
      const key = config.params?.tenant_id ?? 'organization';
      const timing = {
        ms: Number.isFinite(ms) && ms >= 0 ? ms : null,
        orders: Number.isFinite(orders) ? orders : 0,
        sequence: started,
      };
      setSnapshot((previous) => {
        const rows = previous?.scopeKey === scopeKey ? previous.rows : {};
        if ((rows[key]?.sequence ?? 0) > started) return previous;
        return { scopeKey, rows: { ...rows, [key]: timing } };
      });
    };
    const response = http.interceptors.response.use(
      (result) => { finish(result.config, result); return result; },
      (error) => { finish(error.config); return Promise.reject(error); }
    );
    return () => {
      active = false;
      http.interceptors.request.eject(request);
      http.interceptors.response.eject(response);
    };
  }, [scopeKey, kind]);

  return snapshot?.scopeKey === scopeKey ? Object.values(snapshot.rows) : [];
}
